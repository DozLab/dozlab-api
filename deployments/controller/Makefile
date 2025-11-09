# Image URL to use for building/pushing image targets
IMG ?= dozman99/dozlab-controller:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./cmd/controller/... ./internal/controller/...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./cmd/controller/... ./internal/controller/...

.PHONY: test
test: fmt vet ## Run tests.
	go test ./internal/controller/... -coverprofile cover.out

##@ Build

.PHONY: build
build: fmt vet ## Build controller binary.
	go build -o bin/controller cmd/controller/main.go

.PHONY: run
run: fmt vet ## Run controller from your host.
	go run cmd/controller/main.go

.PHONY: docker-build
docker-build: ## Build docker image with the controller.
	docker build -t ${IMG} -f deployments/controller/Dockerfile .

.PHONY: docker-push
docker-push: ## Push docker image with the controller.
	docker push ${IMG}

##@ Deployment

.PHONY: install-crd
install-crd: ## Install CRDs into the K8s cluster.
	kubectl apply -f deployments/controller/crd.yaml

.PHONY: uninstall-crd
uninstall-crd: ## Uninstall CRDs from the K8s cluster.
	kubectl delete -f deployments/controller/crd.yaml

.PHONY: deploy
deploy: ## Deploy controller to the K8s cluster.
	kubectl apply -f deployments/controller/rbac.yaml
	kubectl apply -f deployments/controller/deployment.yaml

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster.
	kubectl delete -f deployments/controller/deployment.yaml
	kubectl delete -f deployments/controller/rbac.yaml

.PHONY: install
install: install-crd deploy ## Install CRDs and deploy controller.

.PHONY: uninstall
uninstall: undeploy uninstall-crd ## Uninstall controller and CRDs.

##@ Testing

.PHONY: test-session
test-session: ## Create a test lab session.
	kubectl apply -f - <<EOF
	apiVersion: dozlab.io/v1
	kind: LabSession
	metadata:
	  name: test-session
	  namespace: default
	spec:
	  userId: "test-user"
	  sessionId: "test-session-123"
	  resources:
	    memory: "4Gi"
	    cpu: "2"
	    storage: "10Gi"
	  config:
	    vsCodePassword: "test-password"
	    enableTerminal: true
	    enableVSCode: true
	    enableSSH: true
	  timeout: "2h"
	EOF

.PHONY: delete-test-session
delete-test-session: ## Delete the test lab session.
	kubectl delete labsession test-session

.PHONY: watch-sessions
watch-sessions: ## Watch lab sessions.
	kubectl get labsessions --watch

.PHONY: logs
logs: ## View controller logs.
	kubectl logs -f -l app=dozlab-controller

##@ Cleanup

.PHONY: clean
clean: ## Clean build artifacts.
	rm -rf bin/
	rm -f cover.out
