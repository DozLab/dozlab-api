package examiner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"dozlab-backend/internal/websocket"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FeedbackService handles real-time feedback and scoring
type FeedbackService struct {
	db       *gorm.DB
	eventBus *websocket.RedisEventBus
}

// NewFeedbackService creates a new feedback service
func NewFeedbackService(db *gorm.DB, eventBus *websocket.RedisEventBus) *FeedbackService {
	return &FeedbackService{
		db:       db,
		eventBus: eventBus,
	}
}

// ProvideRealTimeFeedback provides immediate feedback based on validation results
func (fs *FeedbackService) ProvideRealTimeFeedback(ctx context.Context, result *ValidationResult) (*RealTimeFeedback, error) {
	// Get the validation rule for context
	var rule ValidationRule
	if err := fs.db.First(&rule, "id = ?", result.RuleID).Error; err != nil {
		return nil, fmt.Errorf("failed to get validation rule: %w", err)
	}

	feedback := &RealTimeFeedback{
		ID:           uuid.New().String(),
		SessionID:    result.SessionID,
		UserID:       result.UserID,
		LabID:        result.LabID,
		TaskID:       result.TaskID,
		RuleID:       result.RuleID,
		Type:         fs.determineFeedbackType(result),
		Severity:     fs.determineSeverity(result, &rule),
		Title:        fs.generateFeedbackTitle(result, &rule),
		Message:      fs.generateFeedbackMessage(result, &rule),
		Suggestions:  fs.generateSuggestions(result, &rule),
		Resources:    fs.generateResources(result, &rule),
		ActionItems:  fs.generateActionItems(result, &rule),
		Timestamp:    time.Now(),
		Acknowledged: false,
	}

	// Save feedback to database
	if err := fs.db.Create(feedback).Error; err != nil {
		return nil, fmt.Errorf("failed to save feedback: %w", err)
	}

	// Publish real-time feedback event
	fs.publishFeedbackEvent(ctx, feedback)

	return feedback, nil
}

// GenerateTaskSummaryFeedback generates comprehensive feedback for task completion
func (fs *FeedbackService) GenerateTaskSummaryFeedback(ctx context.Context, taskValidation *TaskValidation) (*TaskSummaryFeedback, error) {
	// Get all validation results for this task
	var results []ValidationResult
	if err := fs.db.Where("session_id = ? AND user_id = ? AND lab_id = ? AND task_id = ?",
		taskValidation.SessionID, taskValidation.UserID, taskValidation.LabID, taskValidation.TaskID).
		Find(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to get validation results: %w", err)
	}

	// Get validation rules for context
	var rules []ValidationRule
	ruleIDs := make([]string, len(results))
	for i, result := range results {
		ruleIDs[i] = result.RuleID
	}
	if err := fs.db.Where("id IN ?", ruleIDs).Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("failed to get validation rules: %w", err)
	}

	// Create rule map for quick lookup
	ruleMap := make(map[string]ValidationRule)
	for _, rule := range rules {
		ruleMap[rule.ID] = rule
	}

	feedback := &TaskSummaryFeedback{
		ID:               uuid.New().String(),
		SessionID:        taskValidation.SessionID,
		UserID:           taskValidation.UserID,
		LabID:            taskValidation.LabID,
		TaskID:           taskValidation.TaskID,
		OverallScore:     taskValidation.Score,
		MaxScore:         taskValidation.MaxScore,
		Percentage:       taskValidation.Percentage,
		Grade:            taskValidation.GetGrade(),
		Status:           string(taskValidation.Status),
		CompletionTime:   taskValidation.CompletionTime,
		Attempts:         taskValidation.Attempts,
		PositiveAspects:  []string{},
		AreasForImprovement: []string{},
		DetailedFeedback: []DetailedRuleFeedback{},
		NextSteps:        []string{},
		LearningOutcomes: []LearningOutcome{},
		Resources:        []FeedbackResource{},
		Timestamp:        time.Now(),
	}

	// Analyze each validation result
	for _, result := range results {
		rule, exists := ruleMap[result.RuleID]
		if !exists {
			continue
		}

		detailedFeedback := DetailedRuleFeedback{
			RuleID:      result.RuleID,
			RuleName:    rule.Name,
			RuleType:    string(rule.Type),
			Passed:      result.Passed,
			Score:       result.Score,
			MaxScore:    result.MaxScore,
			Message:     result.Message,
			Suggestions: fs.generateDetailedSuggestions(&result, &rule),
		}

		if result.Passed {
			feedback.PositiveAspects = append(feedback.PositiveAspects, 
				fmt.Sprintf("✓ %s - Completed successfully", rule.Name))
		} else {
			feedback.AreasForImprovement = append(feedback.AreasForImprovement, 
				fmt.Sprintf("✗ %s - Needs attention", rule.Name))
			
			// Add specific improvement suggestions
			detailedFeedback.ImprovementActions = fs.generateImprovementActions(&result, &rule)
		}

		feedback.DetailedFeedback = append(feedback.DetailedFeedback, detailedFeedback)
	}

	// Generate overall feedback based on performance
	fs.generateOverallTaskFeedback(feedback, taskValidation)

	// Save feedback
	if err := fs.db.Create(feedback).Error; err != nil {
		return nil, fmt.Errorf("failed to save task summary feedback: %w", err)
	}

	// Publish feedback event
	fs.publishTaskSummaryFeedbackEvent(ctx, feedback)

	return feedback, nil
}

// CalculateProgressiveScore calculates score with progressive difficulty
func (fs *FeedbackService) CalculateProgressiveScore(ctx context.Context, sessionID, userID, labID string) (*ProgressiveScore, error) {
	// Get all task validations for the lab
	var taskValidations []TaskValidation
	if err := fs.db.Where("session_id = ? AND user_id = ? AND lab_id = ?", 
		sessionID, userID, labID).Order("created_at ASC").Find(&taskValidations).Error; err != nil {
		return nil, fmt.Errorf("failed to get task validations: %w", err)
	}

	if len(taskValidations) == 0 {
		return &ProgressiveScore{
			SessionID: sessionID,
			UserID:    userID,
			LabID:     labID,
		}, nil
	}

	score := &ProgressiveScore{
		SessionID:        sessionID,
		UserID:           userID,
		LabID:            labID,
		TaskScores:       []TaskScore{},
		DifficultyBonus:  0,
		TimeBonus:        0,
		ConsistencyBonus: 0,
		TotalScore:       0,
		UpdatedAt:        time.Now(),
	}

	var totalRawScore, totalMaxScore float64
	var totalTime int32
	var consistencyScore float64

	// Calculate task scores with progressive difficulty
	for i, tv := range taskValidations {
		taskScore := TaskScore{
			TaskID:           tv.TaskID,
			RawScore:         tv.Score,
			MaxScore:         tv.MaxScore,
			DifficultyMultiplier: 1.0 + float64(i)*0.1, // Progressive difficulty
			TimeBonus:        fs.calculateTimeBonus(tv.CompletionTime, i),
			AdjustedScore:    0,
		}

		// Apply difficulty multiplier
		taskScore.AdjustedScore = taskScore.RawScore * taskScore.DifficultyMultiplier

		// Add time bonus
		taskScore.AdjustedScore += taskScore.TimeBonus

		score.TaskScores = append(score.TaskScores, taskScore)
		
		totalRawScore += tv.Score
		totalMaxScore += tv.MaxScore
		totalTime += tv.CompletionTime

		// Track consistency (success rate)
		if tv.Percentage >= 70 {
			consistencyScore += 1.0
		}
	}

	// Calculate bonuses
	if totalMaxScore > 0 {
		score.DifficultyBonus = (totalRawScore / totalMaxScore) * 10 // Up to 10 points
	}

	// Time bonus - reward efficiency
	avgTime := float64(totalTime) / float64(len(taskValidations))
	if avgTime < 300 { // Less than 5 minutes average
		score.TimeBonus = 5
	} else if avgTime < 600 { // Less than 10 minutes average
		score.TimeBonus = 3
	} else if avgTime < 900 { // Less than 15 minutes average
		score.TimeBonus = 1
	}

	// Consistency bonus - reward steady performance
	consistencyRate := consistencyScore / float64(len(taskValidations))
	score.ConsistencyBonus = consistencyRate * 5 // Up to 5 points

	// Calculate total score
	for _, taskScore := range score.TaskScores {
		score.TotalScore += taskScore.AdjustedScore
	}
	score.TotalScore += score.DifficultyBonus + score.TimeBonus + score.ConsistencyBonus

	return score, nil
}

// GeneratePersonalizedRecommendations generates personalized learning recommendations
func (fs *FeedbackService) GeneratePersonalizedRecommendations(ctx context.Context, userID string) (*PersonalizedRecommendations, error) {
	// Get user's recent performance across labs
	var taskValidations []TaskValidation
	if err := fs.db.Where("user_id = ? AND created_at > ?", 
		userID, time.Now().AddDate(0, 0, -30)).
		Order("created_at DESC").Limit(50).Find(&taskValidations).Error; err != nil {
		return nil, fmt.Errorf("failed to get user performance data: %w", err)
	}

	recommendations := &PersonalizedRecommendations{
		UserID:           userID,
		RecommendedLabs:  []LabRecommendation{},
		SkillGaps:        []SkillGap{},
		StrengthAreas:    []StrengthArea{},
		LearningPath:     []LearningStep{},
		StudyPlan:        StudyPlan{},
		GeneratedAt:      time.Now(),
	}

	if len(taskValidations) == 0 {
		// New user recommendations
		recommendations.RecommendedLabs = fs.getBeginnerLabs()
		return recommendations, nil
	}

	// Analyze performance patterns
	performanceAnalysis := fs.analyzePerformancePatterns(taskValidations)
	
	// Identify skill gaps
	recommendations.SkillGaps = fs.identifySkillGaps(performanceAnalysis)
	
	// Identify strength areas
	recommendations.StrengthAreas = fs.identifyStrengthAreas(performanceAnalysis)
	
	// Generate recommended labs
	recommendations.RecommendedLabs = fs.generateLabRecommendations(performanceAnalysis)
	
	// Create learning path
	recommendations.LearningPath = fs.createLearningPath(recommendations.SkillGaps, recommendations.StrengthAreas)
	
	// Generate study plan
	recommendations.StudyPlan = fs.generateStudyPlan(recommendations.SkillGaps, recommendations.LearningPath)

	return recommendations, nil
}

// GetLeaderboardFeedback provides competitive feedback based on leaderboard position
func (fs *FeedbackService) GetLeaderboardFeedback(ctx context.Context, userID, labID string) (*LeaderboardFeedback, error) {
	// Get user's score for the lab
	var userValidation LabValidation
	if err := fs.db.Where("user_id = ? AND lab_id = ?", userID, labID).
		First(&userValidation).Error; err != nil {
		return nil, fmt.Errorf("failed to get user lab validation: %w", err)
	}

	// Get all completions for this lab
	var allValidations []LabValidation
	if err := fs.db.Where("lab_id = ? AND status = ?", labID, LabValidationStatusCompleted).
		Order("final_percentage DESC, duration ASC").Find(&allValidations).Error; err != nil {
		return nil, fmt.Errorf("failed to get lab leaderboard: %w", err)
	}

	feedback := &LeaderboardFeedback{
		UserID:        userID,
		LabID:         labID,
		UserScore:     userValidation.FinalPercentage,
		Position:      1,
		TotalParticipants: len(allValidations),
		Percentile:    0,
		Achievements:  []Achievement{},
		Comparisons:   ComparisonStats{},
		UpdatedAt:     time.Now(),
	}

	// Find user's position
	for i, validation := range allValidations {
		if validation.UserID == userID {
			feedback.Position = i + 1
			break
		}
	}

	// Calculate percentile
	if len(allValidations) > 0 {
		feedback.Percentile = float64(len(allValidations)-feedback.Position+1) / float64(len(allValidations)) * 100
	}

	// Generate achievements
	feedback.Achievements = fs.generateAchievements(&userValidation, feedback.Position, len(allValidations))

	// Calculate comparison stats
	feedback.Comparisons = fs.calculateComparisonStats(&userValidation, allValidations)

	return feedback, nil
}

// Helper methods for feedback generation

func (fs *FeedbackService) determineFeedbackType(result *ValidationResult) FeedbackType {
	if result.Passed {
		return FeedbackTypeSuccess
	}
	if result.Status == ValidationStatusTimeout {
		return FeedbackTypeWarning
	}
	return FeedbackTypeError
}

func (fs *FeedbackService) determineSeverity(result *ValidationResult, rule *ValidationRule) FeedbackSeverity {
	if !result.Passed && rule.Required {
		return FeedbackSeverityHigh
	}
	if !result.Passed {
		return FeedbackSeverityMedium
	}
	return FeedbackSeverityLow
}

func (fs *FeedbackService) generateFeedbackTitle(result *ValidationResult, rule *ValidationRule) string {
	if result.Passed {
		return fmt.Sprintf("✅ %s - Completed", rule.Name)
	}
	return fmt.Sprintf("❌ %s - Failed", rule.Name)
}

func (fs *FeedbackService) generateFeedbackMessage(result *ValidationResult, rule *ValidationRule) string {
	if result.Passed {
		return fmt.Sprintf("Great job! You successfully completed the validation: %s", rule.Description)
	}
	
	baseMessage := fmt.Sprintf("The validation '%s' did not pass. %s", rule.Name, result.Message)
	
	// Add specific guidance based on validation type
	switch rule.Type {
	case ValidationTypeFileExists:
		return baseMessage + " Please ensure the file exists in the correct location."
	case ValidationTypeFileContent:
		return baseMessage + " Please check the file content matches the requirements."
	case ValidationTypeCommandOutput:
		return baseMessage + " Please verify the command produces the expected output."
	default:
		return baseMessage
	}
}

func (fs *FeedbackService) generateSuggestions(result *ValidationResult, rule *ValidationRule) []string {
	if result.Passed {
		return []string{}
	}

	suggestions := []string{}
	
	switch rule.Type {
	case ValidationTypeFileExists:
		suggestions = append(suggestions, "Check if the file path is correct")
		suggestions = append(suggestions, "Verify you're in the right directory")
		suggestions = append(suggestions, "Make sure you created the file with the exact name specified")
		
	case ValidationTypeFileContent:
		suggestions = append(suggestions, "Review the file content requirements")
		suggestions = append(suggestions, "Check for typos or formatting issues")
		suggestions = append(suggestions, "Ensure the content matches exactly what's expected")
		
	case ValidationTypeCommandOutput:
		suggestions = append(suggestions, "Double-check the command syntax")
		suggestions = append(suggestions, "Verify all required parameters are provided")
		suggestions = append(suggestions, "Check if dependencies are installed")
		
	case ValidationTypeHTTPResponse:
		suggestions = append(suggestions, "Ensure the server is running")
		suggestions = append(suggestions, "Check the URL and port number")
		suggestions = append(suggestions, "Verify the service is properly configured")
		
	default:
		suggestions = append(suggestions, "Review the task requirements carefully")
		suggestions = append(suggestions, "Check the lab instructions for guidance")
	}

	return suggestions
}

func (fs *FeedbackService) generateResources(result *ValidationResult, rule *ValidationRule) []FeedbackResource {
	resources := []FeedbackResource{}
	
	// Add type-specific resources
	switch rule.Type {
	case ValidationTypeFileExists, ValidationTypeFileContent:
		resources = append(resources, FeedbackResource{
			Title:       "File Operations Guide",
			URL:         "https://docs.dozlab.com/guides/file-operations",
			Type:        "documentation",
			Description: "Learn about file creation and management",
		})
		
	case ValidationTypeCommandOutput:
		resources = append(resources, FeedbackResource{
			Title:       "Command Line Tutorial",
			URL:         "https://docs.dozlab.com/tutorials/command-line",
			Type:        "tutorial",
			Description: "Master command line operations",
		})
		
	case ValidationTypeHTTPResponse:
		resources = append(resources, FeedbackResource{
			Title:       "Web Services Guide",
			URL:         "https://docs.dozlab.com/guides/web-services",
			Type:        "documentation",
			Description: "Understanding web service development",
		})
	}

	return resources
}

func (fs *FeedbackService) generateActionItems(result *ValidationResult, rule *ValidationRule) []ActionItem {
	if result.Passed {
		return []ActionItem{}
	}

	items := []ActionItem{}
	
	switch rule.Type {
	case ValidationTypeFileExists:
		items = append(items, ActionItem{
			Action:      "Create the required file",
			Priority:    "High",
			Estimated:   "2 minutes",
			Description: fmt.Sprintf("Create file: %s", rule.Condition.FilePath),
		})
		
	case ValidationTypeFileContent:
		items = append(items, ActionItem{
			Action:      "Update file content",
			Priority:    "High",
			Estimated:   "5 minutes",
			Description: "Modify the file to include the required content",
		})
		
	case ValidationTypeCommandOutput:
		items = append(items, ActionItem{
			Action:      "Fix command execution",
			Priority:    "Medium",
			Estimated:   "3 minutes",
			Description: "Ensure the command produces the expected output",
		})
	}

	return items
}

func (fs *FeedbackService) generateDetailedSuggestions(result *ValidationResult, rule *ValidationRule) []string {
	suggestions := fs.generateSuggestions(result, rule)
	
	// Add more detailed, context-specific suggestions
	if !result.Passed && result.Details.Error != "" {
		suggestions = append(suggestions, fmt.Sprintf("Error details: %s", result.Details.Error))
	}
	
	if result.Details.CommandOutput != nil && result.Details.CommandOutput.Stderr != "" {
		suggestions = append(suggestions, fmt.Sprintf("Command error: %s", result.Details.CommandOutput.Stderr))
	}

	return suggestions
}

func (fs *FeedbackService) generateImprovementActions(result *ValidationResult, rule *ValidationRule) []string {
	actions := []string{}
	
	switch rule.Type {
	case ValidationTypeFileExists:
		actions = append(actions, "Create the missing file in the specified location")
		
	case ValidationTypeFileContent:
		if rule.Condition.Pattern != "" {
			actions = append(actions, fmt.Sprintf("Ensure file content matches pattern: %s", rule.Condition.Pattern))
		}
		if rule.Condition.Expected != "" {
			actions = append(actions, fmt.Sprintf("Include the text: %s", rule.Condition.Expected))
		}
		
	case ValidationTypeCommandOutput:
		actions = append(actions, "Debug the command execution")
		actions = append(actions, "Check for missing dependencies or configuration")
	}

	return actions
}

func (fs *FeedbackService) generateOverallTaskFeedback(feedback *TaskSummaryFeedback, taskValidation *TaskValidation) {
	// Generate next steps based on performance
	if taskValidation.Percentage >= 90 {
		feedback.NextSteps = append(feedback.NextSteps, "Excellent work! You're ready for the next challenge.")
		feedback.NextSteps = append(feedback.NextSteps, "Consider exploring advanced topics in this area.")
	} else if taskValidation.Percentage >= 70 {
		feedback.NextSteps = append(feedback.NextSteps, "Good job! Review any failed validations and retry if needed.")
		feedback.NextSteps = append(feedback.NextSteps, "Focus on the areas that need improvement.")
	} else {
		feedback.NextSteps = append(feedback.NextSteps, "This task needs more work. Please review the requirements carefully.")
		feedback.NextSteps = append(feedback.NextSteps, "Consider reviewing the learning materials before retrying.")
	}

	// Add learning outcomes based on completed validations
	passedCount := float64(taskValidation.PassedRules)
	totalCount := float64(taskValidation.TotalRules)
	
	if totalCount > 0 {
		masteryLevel := (passedCount / totalCount) * 100
		outcome := LearningOutcome{
			Objective:   "Task Completion",
			Achieved:    masteryLevel >= 70,
			Proficiency: masteryLevel,
		}
		
		if masteryLevel >= 90 {
			outcome.Evidence = "Demonstrated excellent understanding with minimal errors"
		} else if masteryLevel >= 70 {
			outcome.Evidence = "Showed good comprehension with some areas for improvement"
		} else {
			outcome.Evidence = "Needs additional practice to master the concepts"
		}
		
		feedback.LearningOutcomes = append(feedback.LearningOutcomes, outcome)
	}
}

func (fs *FeedbackService) calculateTimeBonus(completionTime int32, taskIndex int) float64 {
	// Expected time increases with task complexity
	expectedTime := float64(300 + taskIndex*60) // Base 5 min + 1 min per task index
	actualTime := float64(completionTime)
	
	if actualTime <= expectedTime*0.7 { // Completed in 70% of expected time
		return 2.0 // Good time bonus
	} else if actualTime <= expectedTime {
		return 1.0 // Small time bonus
	}
	return 0 // No bonus for taking longer than expected
}

func (fs *FeedbackService) analyzePerformancePatterns(validations []TaskValidation) PerformanceAnalysis {
	analysis := PerformanceAnalysis{
		TotalTasks:     len(validations),
		SuccessRate:    0,
		AverageScore:   0,
		AverageTime:    0,
		SkillAreas:     make(map[string]float64),
		WeakAreas:      []string{},
		StrongAreas:    []string{},
	}

	if len(validations) == 0 {
		return analysis
	}

	var totalScore, totalTime float64
	var successCount int

	for _, tv := range validations {
		totalScore += tv.Percentage
		totalTime += float64(tv.CompletionTime)
		
		if tv.IsSuccessful() {
			successCount++
		}

		// Analyze by task type (simplified - would need more context in real implementation)
		analysis.SkillAreas["general"] += tv.Percentage
	}

	analysis.SuccessRate = float64(successCount) / float64(len(validations)) * 100
	analysis.AverageScore = totalScore / float64(len(validations))
	analysis.AverageTime = totalTime / float64(len(validations))

	// Identify weak and strong areas
	for skill, avgScore := range analysis.SkillAreas {
		avgScore = avgScore / float64(len(validations))
		if avgScore < 60 {
			analysis.WeakAreas = append(analysis.WeakAreas, skill)
		} else if avgScore >= 85 {
			analysis.StrongAreas = append(analysis.StrongAreas, skill)
		}
	}

	return analysis
}

func (fs *FeedbackService) identifySkillGaps(analysis PerformanceAnalysis) []SkillGap {
	gaps := []SkillGap{}
	
	for _, weakArea := range analysis.WeakAreas {
		gap := SkillGap{
			Skill:       weakArea,
			CurrentLevel: "Beginner",
			TargetLevel: "Intermediate",
			Priority:    "High",
			Recommendations: []string{
				fmt.Sprintf("Focus on %s fundamentals", weakArea),
				fmt.Sprintf("Practice %s exercises", weakArea),
			},
		}
		gaps = append(gaps, gap)
	}

	// Add general gaps based on success rate
	if analysis.SuccessRate < 70 {
		gaps = append(gaps, SkillGap{
			Skill:       "Problem Solving",
			CurrentLevel: "Beginner",
			TargetLevel: "Intermediate",
			Priority:    "High",
			Recommendations: []string{
				"Practice breaking down complex problems",
				"Work on debugging techniques",
			},
		})
	}

	return gaps
}

func (fs *FeedbackService) identifyStrengthAreas(analysis PerformanceAnalysis) []StrengthArea {
	strengths := []StrengthArea{}
	
	for _, strongArea := range analysis.StrongAreas {
		strength := StrengthArea{
			Skill:       strongArea,
			Proficiency: 85, // Simplified
			Evidence:    fmt.Sprintf("Consistently high performance in %s tasks", strongArea),
			Opportunities: []string{
				fmt.Sprintf("Mentor others in %s", strongArea),
				fmt.Sprintf("Explore advanced %s topics", strongArea),
			},
		}
		strengths = append(strengths, strength)
	}

	return strengths
}

func (fs *FeedbackService) generateLabRecommendations(analysis PerformanceAnalysis) []LabRecommendation {
	recommendations := []LabRecommendation{}
	
	// Recommend labs based on weak areas
	for _, weakArea := range analysis.WeakAreas {
		rec := LabRecommendation{
			LabID:       fmt.Sprintf("intro-%s", weakArea),
			Title:       fmt.Sprintf("Introduction to %s", strings.Title(weakArea)),
			Difficulty:  "Beginner",
			Reason:      fmt.Sprintf("Strengthen your %s skills", weakArea),
			Priority:    "High",
			EstimatedTime: 60,
		}
		recommendations = append(recommendations, rec)
	}

	// Recommend advanced labs for strong areas
	for _, strongArea := range analysis.StrongAreas {
		rec := LabRecommendation{
			LabID:       fmt.Sprintf("advanced-%s", strongArea),
			Title:       fmt.Sprintf("Advanced %s", strings.Title(strongArea)),
			Difficulty:  "Advanced",
			Reason:      fmt.Sprintf("Build on your %s expertise", strongArea),
			Priority:    "Medium",
			EstimatedTime: 120,
		}
		recommendations = append(recommendations, rec)
	}

	return recommendations
}

func (fs *FeedbackService) createLearningPath(gaps []SkillGap, strengths []StrengthArea) []LearningStep {
	steps := []LearningStep{}
	
	// Address high-priority gaps first
	for _, gap := range gaps {
		if gap.Priority == "High" {
			step := LearningStep{
				Order:       len(steps) + 1,
				Title:       fmt.Sprintf("Master %s Fundamentals", gap.Skill),
				Description: fmt.Sprintf("Build strong foundation in %s", gap.Skill),
				EstimatedTime: 120,
				Prerequisites: []string{},
				Resources:   []string{
					fmt.Sprintf("%s tutorial", gap.Skill),
					fmt.Sprintf("%s practice exercises", gap.Skill),
				},
			}
			steps = append(steps, step)
		}
	}

	// Build on strengths
	for _, strength := range strengths {
		step := LearningStep{
			Order:       len(steps) + 1,
			Title:       fmt.Sprintf("Advanced %s Techniques", strength.Skill),
			Description: fmt.Sprintf("Deepen your expertise in %s", strength.Skill),
			EstimatedTime: 180,
			Prerequisites: []string{fmt.Sprintf("Basic %s knowledge", strength.Skill)},
			Resources:   []string{
				fmt.Sprintf("Advanced %s guide", strength.Skill),
				fmt.Sprintf("%s best practices", strength.Skill),
			},
		}
		steps = append(steps, step)
	}

	return steps
}

func (fs *FeedbackService) generateStudyPlan(gaps []SkillGap, path []LearningStep) StudyPlan {
	plan := StudyPlan{
		WeeklyHours:     5,
		Duration:        4, // 4 weeks
		FocusAreas:      []string{},
		WeeklyGoals:     []string{},
		Milestones:      []Milestone{},
	}

	// Extract focus areas from gaps
	for _, gap := range gaps {
		plan.FocusAreas = append(plan.FocusAreas, gap.Skill)
	}

	// Generate weekly goals
	plan.WeeklyGoals = []string{
		"Complete foundation exercises",
		"Practice intermediate concepts",
		"Work on advanced topics",
		"Review and consolidate learning",
	}

	// Create milestones
	for i, step := range path {
		if i < 4 { // Limit to 4 milestones
			milestone := Milestone{
				Week:        i + 1,
				Goal:        step.Title,
				Success:     fmt.Sprintf("Successfully complete %s", step.Title),
				Assessment:  fmt.Sprintf("Pass assessment for %s", step.Title),
			}
			plan.Milestones = append(plan.Milestones, milestone)
		}
	}

	return plan
}

func (fs *FeedbackService) getBeginnerLabs() []LabRecommendation {
	return []LabRecommendation{
		{
			LabID:       "intro-programming",
			Title:       "Introduction to Programming",
			Difficulty:  "Beginner",
			Reason:      "Perfect starting point for new learners",
			Priority:    "High",
			EstimatedTime: 90,
		},
		{
			LabID:       "basic-web-dev",
			Title:       "Basic Web Development",
			Difficulty:  "Beginner",
			Reason:      "Learn fundamental web technologies",
			Priority:    "High",
			EstimatedTime: 120,
		},
	}
}

func (fs *FeedbackService) generateAchievements(validation *LabValidation, position int, totalParticipants int) []Achievement {
	achievements := []Achievement{}
	
	if validation.FinalPercentage == 100 {
		achievements = append(achievements, Achievement{
			ID:          "perfect-score",
			Title:       "Perfect Score!",
			Description: "Achieved 100% completion",
			Icon:        "🏆",
			Rarity:      "Legendary",
		})
	}
	
	if position == 1 {
		achievements = append(achievements, Achievement{
			ID:          "first-place",
			Title:       "Champion",
			Description: "First place in the lab",
			Icon:        "👑",
			Rarity:      "Epic",
		})
	} else if position <= 3 {
		achievements = append(achievements, Achievement{
			ID:          "top-three",
			Title:       "Top Performer",
			Description: "Finished in top 3",
			Icon:        "🥉",
			Rarity:      "Rare",
		})
	}
	
	if validation.Duration <= 30 { // 30 minutes
		achievements = append(achievements, Achievement{
			ID:          "speed-demon",
			Title:       "Speed Demon",
			Description: "Completed lab in record time",
			Icon:        "⚡",
			Rarity:      "Rare",
		})
	}

	return achievements
}

func (fs *FeedbackService) calculateComparisonStats(userValidation *LabValidation, allValidations []LabValidation) ComparisonStats {
	if len(allValidations) == 0 {
		return ComparisonStats{}
	}

	var scores []float64
	var times []int32
	
	for _, v := range allValidations {
		scores = append(scores, v.FinalPercentage)
		times = append(times, v.Duration)
	}
	
	sort.Float64s(scores)
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })

	stats := ComparisonStats{
		AverageScore: calculateAverage(scores),
		MedianScore:  calculateMedian(scores),
		AverageTime:  calculateAverageInt32(times),
		MedianTime:   calculateMedianInt32(times),
	}

	// User vs average
	stats.ScoreVsAverage = userValidation.FinalPercentage - stats.AverageScore
	stats.TimeVsAverage = float64(userValidation.Duration) - stats.AverageTime

	return stats
}

// Utility functions

func calculateAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func calculateMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	n := len(values)
	if n%2 == 0 {
		return (values[n/2-1] + values[n/2]) / 2
	}
	return values[n/2]
}

func calculateAverageInt32(values []int32) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := int32(0)
	for _, v := range values {
		sum += v
	}
	return float64(sum) / float64(len(values))
}

func calculateMedianInt32(values []int32) float64 {
	if len(values) == 0 {
		return 0
	}
	n := len(values)
	if n%2 == 0 {
		return float64(values[n/2-1]+values[n/2]) / 2
	}
	return float64(values[n/2])
}

func (fs *FeedbackService) publishFeedbackEvent(ctx context.Context, feedback *RealTimeFeedback) {
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      "feedback.realtime",
		Source:    "examiner",
		SessionID: feedback.SessionID,
		UserID:    feedback.UserID,
		Data: map[string]interface{}{
			"feedback": feedback,
		},
		Timestamp: time.Now(),
	}

	if err := fs.eventBus.Publish(ctx, event); err != nil {
		fmt.Printf("Failed to publish feedback event: %v\n", err)
	}
}

func (fs *FeedbackService) publishTaskSummaryFeedbackEvent(ctx context.Context, feedback *TaskSummaryFeedback) {
	event := &websocket.Event{
		ID:        uuid.New().String(),
		Type:      "feedback.task_summary",
		Source:    "examiner",
		SessionID: feedback.SessionID,
		UserID:    feedback.UserID,
		Data: map[string]interface{}{
			"feedback": feedback,
		},
		Timestamp: time.Now(),
	}

	if err := fs.eventBus.Publish(ctx, event); err != nil {
		fmt.Printf("Failed to publish task summary feedback event: %v\n", err)
	}
}