package examiner

import "time"

// Real-time feedback models

// RealTimeFeedback represents immediate feedback for validation results
type RealTimeFeedback struct {
	ID           string            `json:"id" gorm:"primary_key"`
	SessionID    string            `json:"session_id" gorm:"index;not null"`
	UserID       string            `json:"user_id" gorm:"index;not null"`
	LabID        string            `json:"lab_id" gorm:"index;not null"`
	TaskID       string            `json:"task_id" gorm:"index;not null"`
	RuleID       string            `json:"rule_id" gorm:"index;not null"`
	Type         FeedbackType      `json:"type" gorm:"not null"`
	Severity     FeedbackSeverity  `json:"severity" gorm:"not null"`
	Title        string            `json:"title" gorm:"not null"`
	Message      string            `json:"message" gorm:"not null"`
	Suggestions  []string          `json:"suggestions" gorm:"type:jsonb"`
	Resources    []FeedbackResource `json:"resources" gorm:"type:jsonb"`
	ActionItems  []ActionItem      `json:"action_items" gorm:"type:jsonb"`
	Timestamp    time.Time         `json:"timestamp" gorm:"autoCreateTime"`
	Acknowledged bool              `json:"acknowledged" gorm:"default:false"`
	AIGenerated  bool              `json:"ai_generated" gorm:"default:false"` // For future AI integration
}

// FeedbackType represents the type of feedback
type FeedbackType string

const (
	FeedbackTypeSuccess FeedbackType = "success"
	FeedbackTypeError   FeedbackType = "error"
	FeedbackTypeWarning FeedbackType = "warning"
	FeedbackTypeInfo    FeedbackType = "info"
	FeedbackTypeHint    FeedbackType = "hint"
	FeedbackTypeTutor   FeedbackType = "tutor"
)

// FeedbackSeverity represents the severity level
type FeedbackSeverity string

const (
	FeedbackSeverityLow    FeedbackSeverity = "low"
	FeedbackSeverityMedium FeedbackSeverity = "medium"
	FeedbackSeverityHigh   FeedbackSeverity = "high"
)

// ActionItem represents a specific action for the user
type ActionItem struct {
	Action      string `json:"action"`
	Priority    string `json:"priority"`
	Estimated   string `json:"estimated_time"`
	Description string `json:"description"`
}

// TaskSummaryFeedback represents comprehensive feedback for task completion
type TaskSummaryFeedback struct {
	ID                  string                  `json:"id" gorm:"primary_key"`
	SessionID           string                  `json:"session_id" gorm:"index;not null"`
	UserID              string                  `json:"user_id" gorm:"index;not null"`
	LabID               string                  `json:"lab_id" gorm:"index;not null"`
	TaskID              string                  `json:"task_id" gorm:"index;not null"`
	OverallScore        float64                 `json:"overall_score"`
	MaxScore            float64                 `json:"max_score"`
	Percentage          float64                 `json:"percentage"`
	Grade               string                  `json:"grade"`
	Status              string                  `json:"status"`
	CompletionTime      int32                   `json:"completion_time"`
	Attempts            int32                   `json:"attempts"`
	PositiveAspects     []string                `json:"positive_aspects" gorm:"type:jsonb"`
	AreasForImprovement []string                `json:"areas_for_improvement" gorm:"type:jsonb"`
	DetailedFeedback    []DetailedRuleFeedback  `json:"detailed_feedback" gorm:"type:jsonb"`
	NextSteps           []string                `json:"next_steps" gorm:"type:jsonb"`
	LearningOutcomes    []LearningOutcome       `json:"learning_outcomes" gorm:"type:jsonb"`
	Resources           []FeedbackResource      `json:"resources" gorm:"type:jsonb"`
	AIInsights          *AIInsights             `json:"ai_insights,omitempty" gorm:"type:jsonb"` // For future AI integration
	Timestamp           time.Time               `json:"timestamp" gorm:"autoCreateTime"`
}

// DetailedRuleFeedback provides specific feedback for each validation rule
type DetailedRuleFeedback struct {
	RuleID             string   `json:"rule_id"`
	RuleName           string   `json:"rule_name"`
	RuleType           string   `json:"rule_type"`
	Passed             bool     `json:"passed"`
	Score              float64  `json:"score"`
	MaxScore           float64  `json:"max_score"`
	Message            string   `json:"message"`
	Suggestions        []string `json:"suggestions"`
	ImprovementActions []string `json:"improvement_actions"`
}

// LearningOutcome represents a learning objective and its achievement
type LearningOutcome struct {
	Objective   string  `json:"objective"`
	Achieved    bool    `json:"achieved"`
	Proficiency float64 `json:"proficiency"` // 0-100
	Evidence    string  `json:"evidence"`
}

// AIInsights represents AI-generated insights for future integration
type AIInsights struct {
	LearningStyle       string            `json:"learning_style,omitempty"`        // Visual, Auditory, Kinesthetic
	ConceptUnderstanding map[string]float64 `json:"concept_understanding,omitempty"` // Concept -> Understanding level
	PredictedDifficulty string            `json:"predicted_difficulty,omitempty"`  // Easy, Medium, Hard
	RecommendedApproach string            `json:"recommended_approach,omitempty"`
	PersonalizedHints   []string          `json:"personalized_hints,omitempty"`
	AdaptiveContent     []string          `json:"adaptive_content,omitempty"`
	ConfidenceScore     float64           `json:"confidence_score,omitempty"` // AI's confidence in insights
}

// Progressive scoring models

// ProgressiveScore represents advanced scoring with bonuses
type ProgressiveScore struct {
	SessionID        string      `json:"session_id"`
	UserID           string      `json:"user_id"`
	LabID            string      `json:"lab_id"`
	TaskScores       []TaskScore `json:"task_scores"`
	DifficultyBonus  float64     `json:"difficulty_bonus"`
	TimeBonus        float64     `json:"time_bonus"`
	ConsistencyBonus float64     `json:"consistency_bonus"`
	AIBonus          float64     `json:"ai_bonus,omitempty"`          // For future AI-assisted learning
	CollaborationBonus float64   `json:"collaboration_bonus,omitempty"` // For peer collaboration
	TotalScore       float64     `json:"total_score"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

// TaskScore represents scoring for individual tasks
type TaskScore struct {
	TaskID               string  `json:"task_id"`
	RawScore             float64 `json:"raw_score"`
	MaxScore             float64 `json:"max_score"`
	DifficultyMultiplier float64 `json:"difficulty_multiplier"`
	TimeBonus            float64 `json:"time_bonus"`
	AdjustedScore        float64 `json:"adjusted_score"`
}

// Personalized recommendation models

// PersonalizedRecommendations represents AI-powered learning recommendations
type PersonalizedRecommendations struct {
	UserID             string               `json:"user_id"`
	RecommendedLabs    []LabRecommendation  `json:"recommended_labs"`
	SkillGaps          []SkillGap          `json:"skill_gaps"`
	StrengthAreas      []StrengthArea      `json:"strength_areas"`
	LearningPath       []LearningStep      `json:"learning_path"`
	StudyPlan          StudyPlan           `json:"study_plan"`
	AIRecommendations  *AIRecommendations  `json:"ai_recommendations,omitempty"` // Future AI integration
	GeneratedAt        time.Time           `json:"generated_at"`
}

// LabRecommendation represents a recommended lab
type LabRecommendation struct {
	LabID         string  `json:"lab_id"`
	Title         string  `json:"title"`
	Difficulty    string  `json:"difficulty"`
	Reason        string  `json:"reason"`
	Priority      string  `json:"priority"`
	EstimatedTime int     `json:"estimated_time"` // minutes
	Prerequisites []string `json:"prerequisites,omitempty"`
	AIConfidence  float64 `json:"ai_confidence,omitempty"` // Future AI integration
}

// SkillGap represents an identified skill gap
type SkillGap struct {
	Skill           string   `json:"skill"`
	CurrentLevel    string   `json:"current_level"`
	TargetLevel     string   `json:"target_level"`
	Priority        string   `json:"priority"`
	Recommendations []string `json:"recommendations"`
	EstimatedTime   int      `json:"estimated_time,omitempty"` // hours to close gap
}

// StrengthArea represents a user's strength
type StrengthArea struct {
	Skill         string   `json:"skill"`
	Proficiency   float64  `json:"proficiency"` // 0-100
	Evidence      string   `json:"evidence"`
	Opportunities []string `json:"opportunities"`
}

// LearningStep represents a step in a personalized learning path
type LearningStep struct {
	Order         int      `json:"order"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	EstimatedTime int      `json:"estimated_time"` // minutes
	Prerequisites []string `json:"prerequisites"`
	Resources     []string `json:"resources"`
}

// StudyPlan represents a personalized study plan
type StudyPlan struct {
	WeeklyHours int         `json:"weekly_hours"`
	Duration    int         `json:"duration"` // weeks
	FocusAreas  []string    `json:"focus_areas"`
	WeeklyGoals []string    `json:"weekly_goals"`
	Milestones  []Milestone `json:"milestones"`
}

// Milestone represents a learning milestone
type Milestone struct {
	Week       int    `json:"week"`
	Goal       string `json:"goal"`
	Success    string `json:"success_criteria"`
	Assessment string `json:"assessment"`
}

// AIRecommendations represents AI-powered recommendations (future feature)
type AIRecommendations struct {
	AdaptiveLearningPath []AdaptiveLearningStep `json:"adaptive_learning_path,omitempty"`
	PersonalizedContent  []ContentRecommendation `json:"personalized_content,omitempty"`
	OptimalSchedule      *LearningSchedule      `json:"optimal_schedule,omitempty"`
	PeerMatching         []PeerMatch            `json:"peer_matching,omitempty"`
	TutorRecommendations []TutorRecommendation  `json:"tutor_recommendations,omitempty"`
	ConfidenceScore      float64                `json:"confidence_score"`
}

// AdaptiveLearningStep represents AI-adaptive learning steps
type AdaptiveLearningStep struct {
	StepID           string            `json:"step_id"`
	Title            string            `json:"title"`
	Difficulty       string            `json:"difficulty"`
	LearningStyle    string            `json:"learning_style"`
	ConceptDependencies []string       `json:"concept_dependencies"`
	AdaptiveContent  []string          `json:"adaptive_content"`
	AIReasoning      string            `json:"ai_reasoning"`
}

// ContentRecommendation represents AI-recommended content
type ContentRecommendation struct {
	ContentID    string  `json:"content_id"`
	ContentType  string  `json:"content_type"` // video, article, exercise, simulation
	Title        string  `json:"title"`
	Relevance    float64 `json:"relevance"`    // 0-1
	Difficulty   string  `json:"difficulty"`
	Reason       string  `json:"reason"`
	AIGenerated  bool    `json:"ai_generated"`
}

// LearningSchedule represents optimal learning schedule
type LearningSchedule struct {
	OptimalTimes    []TimeSlot `json:"optimal_times"`
	SessionDuration int        `json:"session_duration"` // minutes
	BreakFrequency  int        `json:"break_frequency"`  // minutes
	ReviewSchedule  []ReviewSession `json:"review_schedule"`
}

// TimeSlot represents optimal learning time
type TimeSlot struct {
	DayOfWeek string `json:"day_of_week"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Reason    string `json:"reason"`
}

// ReviewSession represents spaced repetition schedule
type ReviewSession struct {
	Concept     string    `json:"concept"`
	ReviewDate  time.Time `json:"review_date"`
	Interval    int       `json:"interval"`    // days
	Difficulty  float64   `json:"difficulty"`  // 0-1
}

// PeerMatch represents recommended peer collaboration
type PeerMatch struct {
	PeerID         string  `json:"peer_id"`
	MatchScore     float64 `json:"match_score"`     // 0-1
	Complementary  bool    `json:"complementary"`   // complementary or similar skills
	Reason         string  `json:"reason"`
	SuggestedLabs  []string `json:"suggested_labs"`
}

// TutorRecommendation represents AI tutor suggestions
type TutorRecommendation struct {
	TutorType       string   `json:"tutor_type"`     // human, ai, peer
	TutorID         string   `json:"tutor_id,omitempty"`
	Specialization  []string `json:"specialization"`
	MatchScore      float64  `json:"match_score"`    // 0-1
	Reason          string   `json:"reason"`
	EstimatedCost   float64  `json:"estimated_cost,omitempty"`
	Availability    string   `json:"availability,omitempty"`
}

// Leaderboard and competitive feedback models

// LeaderboardFeedback represents competitive feedback
type LeaderboardFeedback struct {
	UserID            string          `json:"user_id"`
	LabID             string          `json:"lab_id"`
	UserScore         float64         `json:"user_score"`
	Position          int             `json:"position"`
	TotalParticipants int             `json:"total_participants"`
	Percentile        float64         `json:"percentile"`
	Achievements      []Achievement   `json:"achievements"`
	Comparisons       ComparisonStats `json:"comparisons"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// Achievement represents a earned achievement
type Achievement struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Rarity      string `json:"rarity"` // Common, Rare, Epic, Legendary
	EarnedAt    time.Time `json:"earned_at,omitempty"`
}

// ComparisonStats represents performance comparisons
type ComparisonStats struct {
	AverageScore   float64 `json:"average_score"`
	MedianScore    float64 `json:"median_score"`
	AverageTime    float64 `json:"average_time"`
	MedianTime     float64 `json:"median_time"`
	ScoreVsAverage float64 `json:"score_vs_average"`
	TimeVsAverage  float64 `json:"time_vs_average"`
}

// Performance analysis models

// PerformanceAnalysis represents user performance analysis
type PerformanceAnalysis struct {
	TotalTasks   int                `json:"total_tasks"`
	SuccessRate  float64            `json:"success_rate"`  // 0-100
	AverageScore float64            `json:"average_score"` // 0-100
	AverageTime  float64            `json:"average_time"`  // seconds
	SkillAreas   map[string]float64 `json:"skill_areas"`   // skill -> average score
	WeakAreas    []string           `json:"weak_areas"`
	StrongAreas  []string           `json:"strong_areas"`
}

// AI-powered tutoring models (future features)

// AITutorSession represents an AI tutoring session
type AITutorSession struct {
	ID              string                `json:"id" gorm:"primary_key"`
	SessionID       string                `json:"session_id" gorm:"index;not null"`
	UserID          string                `json:"user_id" gorm:"index;not null"`
	LabID           string                `json:"lab_id" gorm:"index;not null"`
	TaskID          string                `json:"task_id,omitempty" gorm:"index"`
	TutorType       string                `json:"tutor_type"`       // adaptive, socratic, guided
	Interactions    []TutorInteraction    `json:"interactions" gorm:"type:jsonb"`
	LearningGoals   []string              `json:"learning_goals" gorm:"type:jsonb"`
	Progress        TutorProgress         `json:"progress" gorm:"type:jsonb"`
	AdaptiveBehavior *AdaptiveBehavior    `json:"adaptive_behavior,omitempty" gorm:"type:jsonb"`
	StartedAt       time.Time             `json:"started_at" gorm:"autoCreateTime"`
	EndedAt         *time.Time            `json:"ended_at,omitempty"`
	UpdatedAt       time.Time             `json:"updated_at" gorm:"autoUpdateTime"`
}

// TutorInteraction represents an interaction between user and AI tutor
type TutorInteraction struct {
	Timestamp    time.Time            `json:"timestamp"`
	Type         string               `json:"type"`         // question, hint, explanation, encouragement
	UserInput    string               `json:"user_input,omitempty"`
	TutorResponse string              `json:"tutor_response"`
	Context      map[string]interface{} `json:"context,omitempty"`
	Effectiveness float64             `json:"effectiveness,omitempty"` // 0-1, measured by user progress
}

// TutorProgress represents learning progress during tutoring
type TutorProgress struct {
	ConceptsMastered  []string           `json:"concepts_mastered"`
	CurrentStruggle   string             `json:"current_struggle,omitempty"`
	ConfidenceLevel   float64            `json:"confidence_level"`   // 0-1
	EngagementLevel   float64            `json:"engagement_level"`   // 0-1
	ProgressMetrics   map[string]float64 `json:"progress_metrics"`   // metric -> value
}

// AdaptiveBehavior represents how AI tutor adapts to user
type AdaptiveBehavior struct {
	LearnedPreferences map[string]string  `json:"learned_preferences"`
	EffectiveStrategies []string          `json:"effective_strategies"`
	AdaptationHistory  []AdaptationEvent `json:"adaptation_history"`
	PersonalityProfile *PersonalityProfile `json:"personality_profile,omitempty"`
}

// AdaptationEvent represents a change in tutoring strategy
type AdaptationEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Trigger   string    `json:"trigger"`   // low_engagement, confusion, success, etc.
	Action    string    `json:"action"`    // change_explanation_style, provide_hint, etc.
	Result    string    `json:"result"`    // improved, no_change, worse
}

// PersonalityProfile represents learned user personality traits
type PersonalityProfile struct {
	LearningStyle      string  `json:"learning_style"`      // visual, auditory, kinesthetic
	MotivationType     string  `json:"motivation_type"`     // achievement, competition, collaboration
	FeedbackPreference string  `json:"feedback_preference"` // immediate, summary, minimal
	ChallengeLevel     string  `json:"challenge_level"`     // conservative, balanced, aggressive
	PatienceLevel      float64 `json:"patience_level"`      // 0-1
	DetailOrientation  float64 `json:"detail_orientation"`  // 0-1
}

// HintSystem represents intelligent hint generation
type HintSystem struct {
	UserID           string      `json:"user_id"`
	LabID            string      `json:"lab_id"`
	TaskID           string      `json:"task_id"`
	RuleID           string      `json:"rule_id,omitempty"`
	AvailableHints   []SmartHint `json:"available_hints"`
	UsedHints        []string    `json:"used_hints"`     // hint IDs
	ProgressiveLevel int         `json:"progressive_level"` // 1=gentle, 5=explicit
	AdaptiveHints    []AdaptiveHint `json:"adaptive_hints,omitempty"`
}

// SmartHint represents an intelligent hint
type SmartHint struct {
	ID              string            `json:"id"`
	Level           int               `json:"level"`           // 1=subtle, 5=explicit
	Type            string            `json:"type"`            // conceptual, procedural, strategic
	Content         string            `json:"content"`
	Prerequisites   []string          `json:"prerequisites"`   // required knowledge
	Triggers        []string          `json:"triggers"`        // when to show this hint
	Context         map[string]interface{} `json:"context"`
	EffectivenessScore float64        `json:"effectiveness_score,omitempty"`
	AIGenerated     bool              `json:"ai_generated"`
}

// AdaptiveHint represents AI-generated contextual hints
type AdaptiveHint struct {
	ID              string            `json:"id"`
	GeneratedFor    string            `json:"generated_for"`    // specific error or context
	Content         string            `json:"content"`
	Confidence      float64           `json:"confidence"`       // 0-1
	Personalization map[string]interface{} `json:"personalization"`
	GeneratedAt     time.Time         `json:"generated_at"`
}

// WorkflowBuilder represents AI-assisted workflow building
type WorkflowBuilder struct {
	UserID          string              `json:"user_id"`
	LabID           string              `json:"lab_id"`
	Goal            string              `json:"goal"`
	GeneratedSteps  []WorkflowStep      `json:"generated_steps"`
	UserModifications []StepModification `json:"user_modifications"`
	AIAssistance    WorkflowAssistance  `json:"ai_assistance"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// WorkflowStep represents a step in an AI-generated workflow
type WorkflowStep struct {
	ID              string            `json:"id"`
	Order           int               `json:"order"`
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Type            string            `json:"type"`            // command, file_check, validation, etc.
	Parameters      map[string]interface{} `json:"parameters"`
	Prerequisites   []string          `json:"prerequisites"`
	EstimatedTime   int               `json:"estimated_time"`  // minutes
	DifficultyLevel int               `json:"difficulty_level"` // 1-5
	AIGenerated     bool              `json:"ai_generated"`
	AIConfidence    float64           `json:"ai_confidence"`   // 0-1
}

// StepModification represents user modifications to AI-generated steps
type StepModification struct {
	StepID       string                 `json:"step_id"`
	Type         string                 `json:"type"`         // edit, delete, reorder, add
	OldValue     map[string]interface{} `json:"old_value,omitempty"`
	NewValue     map[string]interface{} `json:"new_value,omitempty"`
	Reason       string                 `json:"reason,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
}

// WorkflowAssistance represents AI assistance during workflow building
type WorkflowAssistance struct {
	Suggestions     []WorkflowSuggestion `json:"suggestions"`
	Alternatives    []AlternativeStep    `json:"alternatives"`
	OptimizationTips []string            `json:"optimization_tips"`
	PotentialIssues []PotentialIssue     `json:"potential_issues"`
}

// WorkflowSuggestion represents AI suggestions for workflow improvement
type WorkflowSuggestion struct {
	Type        string  `json:"type"`        // add_step, reorder, optimize
	Description string  `json:"description"`
	Impact      string  `json:"impact"`      // low, medium, high
	Confidence  float64 `json:"confidence"`  // 0-1
	Reasoning   string  `json:"reasoning"`
}

// AlternativeStep represents alternative approaches for a step
type AlternativeStep struct {
	OriginalStepID string     `json:"original_step_id"`
	Alternative    WorkflowStep `json:"alternative"`
	Advantages     []string   `json:"advantages"`
	Disadvantages  []string   `json:"disadvantages"`
}

// PotentialIssue represents potential problems AI identified
type PotentialIssue struct {
	StepID      string  `json:"step_id,omitempty"`
	Issue       string  `json:"issue"`
	Severity    string  `json:"severity"`    // low, medium, high, critical
	Probability float64 `json:"probability"` // 0-1
	Mitigation  string  `json:"mitigation"`
}