// Copyright 2025 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
	"github.com/openchoreo/openchoreo/internal/server/middleware/logger"
)

// ResourceCRUDResponse represents the response for resource CRUD operations
type ResourceCRUDResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Operation  string `json:"operation,omitempty"` // "created", "updated", "deleted", "not_found"
}

// openChoreoGVK creates a GroupVersionKind for an OpenChoreo resource
func openChoreoGVK(kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "openchoreo.dev",
		Version: "v1alpha1",
		Kind:    kind,
	}
}

// buildUnstructuredRef creates an unstructured object with the specified GVK, namespace, and name
func buildUnstructuredRef(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	return obj
}

// getResourceByGVK fetches a resource from Kubernetes using the specified GVK, namespace, and name
func (h *Handler) getResourceByGVK(ctx context.Context, gvk schema.GroupVersionKind, namespace, name string) (*unstructured.Unstructured, error) {
	k8sClient := h.services.GetKubernetesClient()

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	err := k8sClient.Get(ctx, namespacedName, obj)
	return obj, err
}

// ========== ComponentType Definition Handlers ==========

// GetComponentTypeDefinition handles GET /api/v1/namespaces/{namespaceName}/component-types/{ctName}/definition
func (h *Handler) GetComponentTypeDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	ctName := r.PathValue("ctName")

	if namespaceName == "" || ctName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "ctName", ctName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and ctName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("ComponentType")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, ctName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("ComponentType not found", "namespace", namespaceName, "name", ctName)
			writeErrorResponse(w, http.StatusNotFound, "ComponentType not found", services.CodeComponentTypeNotFound)
			return
		}
		log.Error("Failed to get ComponentType", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get ComponentType", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved ComponentType definition", "namespace", namespaceName, "name", ctName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateComponentTypeDefinition handles PUT /api/v1/namespaces/{namespaceName}/component-types/{ctName}/definition
func (h *Handler) UpdateComponentTypeDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	ctName := r.PathValue("ctName")

	if namespaceName == "" || ctName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "ctName", ctName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and ctName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	// Validate the resource
	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	// Validate kind matches
	if kind != "ComponentType" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be ComponentType", services.CodeInvalidInput)
		return
	}

	// Ensure namespace and name in URL match the resource
	if name != ctName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}

	// Set namespace from URL
	unstructuredObj.SetNamespace(namespaceName)

	// Handle namespace logic
	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	// Apply the resource
	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply ComponentType", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply ComponentType: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("ComponentType applied successfully", "namespace", namespaceName, "name", ctName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteComponentTypeDefinition handles DELETE /api/v1/namespaces/{namespaceName}/component-types/{ctName}/definition
func (h *Handler) DeleteComponentTypeDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	ctName := r.PathValue("ctName")

	if namespaceName == "" || ctName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "ctName", ctName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and ctName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("ComponentType")
	obj := buildUnstructuredRef(gvk, namespaceName, ctName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete ComponentType", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete ComponentType: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       ctName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("ComponentType deleted", "namespace", namespaceName, "name", ctName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== Trait Definition Handlers ==========

// GetTraitDefinition handles GET /api/v1/namespaces/{namespaceName}/traits/{traitName}/definition
func (h *Handler) GetTraitDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	traitName := r.PathValue("traitName")

	if namespaceName == "" || traitName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "traitName", traitName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and traitName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Trait")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, traitName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("Trait not found", "namespace", namespaceName, "name", traitName)
			writeErrorResponse(w, http.StatusNotFound, "Trait not found", services.CodeNotFound)
			return
		}
		log.Error("Failed to get Trait", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get Trait", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved Trait definition", "namespace", namespaceName, "name", traitName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateTraitDefinition handles PUT /api/v1/namespaces/{namespaceName}/traits/{traitName}/definition
func (h *Handler) UpdateTraitDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	traitName := r.PathValue("traitName")

	if namespaceName == "" || traitName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "traitName", traitName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and traitName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "Trait" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be Trait", services.CodeInvalidInput)
		return
	}

	if name != traitName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply Trait", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply Trait: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Trait applied successfully", "namespace", namespaceName, "name", traitName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteTraitDefinition handles DELETE /api/v1/namespaces/{namespaceName}/traits/{traitName}/definition
func (h *Handler) DeleteTraitDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	traitName := r.PathValue("traitName")

	if namespaceName == "" || traitName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "traitName", traitName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and traitName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Trait")
	obj := buildUnstructuredRef(gvk, namespaceName, traitName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete Trait", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete Trait: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       traitName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Trait deleted", "namespace", namespaceName, "name", traitName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== Workflow Definition Handlers ==========

// GetWorkflowDefinition handles GET /api/v1/namespaces/{namespaceName}/workflows/{workflowName}/definition
func (h *Handler) GetWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	workflowName := r.PathValue("workflowName")

	if namespaceName == "" || workflowName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "workflowName", workflowName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and workflowName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Workflow")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, workflowName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("Workflow not found", "namespace", namespaceName, "name", workflowName)
			writeErrorResponse(w, http.StatusNotFound, "Workflow not found", services.CodeNotFound)
			return
		}
		log.Error("Failed to get Workflow", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get Workflow", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved Workflow definition", "namespace", namespaceName, "name", workflowName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateWorkflowDefinition handles PUT /api/v1/namespaces/{namespaceName}/workflows/{workflowName}/definition
func (h *Handler) UpdateWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	workflowName := r.PathValue("workflowName")

	if namespaceName == "" || workflowName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "workflowName", workflowName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and workflowName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "Workflow" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be Workflow", services.CodeInvalidInput)
		return
	}

	if name != workflowName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply Workflow", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply Workflow: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Workflow applied successfully", "namespace", namespaceName, "name", workflowName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteWorkflowDefinition handles DELETE /api/v1/namespaces/{namespaceName}/workflows/{workflowName}/definition
func (h *Handler) DeleteWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	workflowName := r.PathValue("workflowName")

	if namespaceName == "" || workflowName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "workflowName", workflowName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and workflowName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Workflow")
	obj := buildUnstructuredRef(gvk, namespaceName, workflowName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete Workflow", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete Workflow: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       workflowName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Workflow deleted", "namespace", namespaceName, "name", workflowName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== ComponentWorkflow Definition Handlers ==========

// GetComponentWorkflowDefinition handles GET /api/v1/namespaces/{namespaceName}/component-workflows/{cwName}/definition
func (h *Handler) GetComponentWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	cwName := r.PathValue("cwName")

	if namespaceName == "" || cwName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "cwName", cwName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and cwName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("ComponentWorkflow")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, cwName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("ComponentWorkflow not found", "namespace", namespaceName, "name", cwName)
			writeErrorResponse(w, http.StatusNotFound, "ComponentWorkflow not found", services.CodeNotFound)
			return
		}
		log.Error("Failed to get ComponentWorkflow", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get ComponentWorkflow", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved ComponentWorkflow definition", "namespace", namespaceName, "name", cwName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateComponentWorkflowDefinition handles PUT /api/v1/namespaces/{namespaceName}/component-workflows/{cwName}/definition
func (h *Handler) UpdateComponentWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	cwName := r.PathValue("cwName")

	if namespaceName == "" || cwName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "cwName", cwName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and cwName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "ComponentWorkflow" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be ComponentWorkflow", services.CodeInvalidInput)
		return
	}

	if name != cwName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply ComponentWorkflow", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply ComponentWorkflow: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("ComponentWorkflow applied successfully", "namespace", namespaceName, "name", cwName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteComponentWorkflowDefinition handles DELETE /api/v1/namespaces/{namespaceName}/component-workflows/{cwName}/definition
func (h *Handler) DeleteComponentWorkflowDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	cwName := r.PathValue("cwName")

	if namespaceName == "" || cwName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "cwName", cwName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and cwName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("ComponentWorkflow")
	obj := buildUnstructuredRef(gvk, namespaceName, cwName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete ComponentWorkflow", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete ComponentWorkflow: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       cwName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("ComponentWorkflow deleted", "namespace", namespaceName, "name", cwName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== Project Definition Handlers ==========

// GetProjectDefinition handles GET /api/v1/namespaces/{namespaceName}/projects/{projectName}/definition
func (h *Handler) GetProjectDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	projectName := r.PathValue("projectName")

	if namespaceName == "" || projectName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "projectName", projectName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and projectName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Project")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, projectName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("Project not found", "namespace", namespaceName, "name", projectName)
			writeErrorResponse(w, http.StatusNotFound, "Project not found", services.CodeProjectNotFound)
			return
		}
		log.Error("Failed to get Project", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get Project", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved Project definition", "namespace", namespaceName, "name", projectName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateProjectDefinition handles PUT /api/v1/namespaces/{namespaceName}/projects/{projectName}/definition
func (h *Handler) UpdateProjectDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	projectName := r.PathValue("projectName")

	if namespaceName == "" || projectName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "projectName", projectName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and projectName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "Project" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be Project", services.CodeInvalidInput)
		return
	}

	if name != projectName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply Project", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply Project: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Project applied successfully", "namespace", namespaceName, "name", projectName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteProjectDefinition handles DELETE /api/v1/namespaces/{namespaceName}/projects/{projectName}/definition
func (h *Handler) DeleteProjectDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	projectName := r.PathValue("projectName")

	if namespaceName == "" || projectName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "projectName", projectName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and projectName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Project")
	obj := buildUnstructuredRef(gvk, namespaceName, projectName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete Project", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete Project: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       projectName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Project deleted", "namespace", namespaceName, "name", projectName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== Component Definition Handlers ==========

// GetComponentDefinition handles GET /api/v1/namespaces/{namespaceName}/components/{componentName}/definition
func (h *Handler) GetComponentDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	componentName := r.PathValue("componentName")

	if namespaceName == "" || componentName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "componentName", componentName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and componentName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Component")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, componentName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("Component not found", "namespace", namespaceName, "name", componentName)
			writeErrorResponse(w, http.StatusNotFound, "Component not found", services.CodeComponentNotFound)
			return
		}
		log.Error("Failed to get Component", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get Component", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved Component definition", "namespace", namespaceName, "name", componentName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateComponentDefinition handles PUT /api/v1/namespaces/{namespaceName}/components/{componentName}/definition
func (h *Handler) UpdateComponentDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	componentName := r.PathValue("componentName")

	if namespaceName == "" || componentName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "componentName", componentName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and componentName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "Component" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be Component", services.CodeInvalidInput)
		return
	}

	if name != componentName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply Component", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply Component: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Component applied successfully", "namespace", namespaceName, "name", componentName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteComponentDefinition handles DELETE /api/v1/namespaces/{namespaceName}/components/{componentName}/definition
func (h *Handler) DeleteComponentDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	componentName := r.PathValue("componentName")

	if namespaceName == "" || componentName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "componentName", componentName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and componentName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Component")
	obj := buildUnstructuredRef(gvk, namespaceName, componentName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete Component", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete Component: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       componentName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Component deleted", "namespace", namespaceName, "name", componentName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== Environment Definition Handlers ==========

// GetEnvironmentDefinition handles GET /api/v1/namespaces/{namespaceName}/environments/{envName}/definition
func (h *Handler) GetEnvironmentDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	envName := r.PathValue("envName")

	if namespaceName == "" || envName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "envName", envName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and envName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Environment")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, envName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("Environment not found", "namespace", namespaceName, "name", envName)
			writeErrorResponse(w, http.StatusNotFound, "Environment not found", services.CodeEnvironmentNotFound)
			return
		}
		log.Error("Failed to get Environment", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get Environment", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved Environment definition", "namespace", namespaceName, "name", envName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateEnvironmentDefinition handles PUT /api/v1/namespaces/{namespaceName}/environments/{envName}/definition
func (h *Handler) UpdateEnvironmentDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	envName := r.PathValue("envName")

	if namespaceName == "" || envName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "envName", envName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and envName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "Environment" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be Environment", services.CodeInvalidInput)
		return
	}

	if name != envName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply Environment", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply Environment: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Environment applied successfully", "namespace", namespaceName, "name", envName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteEnvironmentDefinition handles DELETE /api/v1/namespaces/{namespaceName}/environments/{envName}/definition
func (h *Handler) DeleteEnvironmentDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	envName := r.PathValue("envName")

	if namespaceName == "" || envName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "envName", envName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and envName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Environment")
	obj := buildUnstructuredRef(gvk, namespaceName, envName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete Environment", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete Environment: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       envName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Environment deleted", "namespace", namespaceName, "name", envName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== DataPlane Definition Handlers ==========

// GetDataPlaneDefinition handles GET /api/v1/namespaces/{namespaceName}/dataplanes/{dpName}/definition
func (h *Handler) GetDataPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	dpName := r.PathValue("dpName")

	if namespaceName == "" || dpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "dpName", dpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and dpName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("DataPlane")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, dpName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("DataPlane not found", "namespace", namespaceName, "name", dpName)
			writeErrorResponse(w, http.StatusNotFound, "DataPlane not found", services.CodeDataPlaneNotFound)
			return
		}
		log.Error("Failed to get DataPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get DataPlane", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved DataPlane definition", "namespace", namespaceName, "name", dpName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateDataPlaneDefinition handles PUT /api/v1/namespaces/{namespaceName}/dataplanes/{dpName}/definition
func (h *Handler) UpdateDataPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	dpName := r.PathValue("dpName")

	if namespaceName == "" || dpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "dpName", dpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and dpName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "DataPlane" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be DataPlane", services.CodeInvalidInput)
		return
	}

	if name != dpName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply DataPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply DataPlane: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("DataPlane applied successfully", "namespace", namespaceName, "name", dpName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteDataPlaneDefinition handles DELETE /api/v1/namespaces/{namespaceName}/dataplanes/{dpName}/definition
func (h *Handler) DeleteDataPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	dpName := r.PathValue("dpName")

	if namespaceName == "" || dpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "dpName", dpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and dpName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("DataPlane")
	obj := buildUnstructuredRef(gvk, namespaceName, dpName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete DataPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete DataPlane: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       dpName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("DataPlane deleted", "namespace", namespaceName, "name", dpName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== BuildPlane Definition Handlers ==========

// GetBuildPlaneDefinition handles GET /api/v1/namespaces/{namespaceName}/buildplanes/{bpName}/definition
func (h *Handler) GetBuildPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	bpName := r.PathValue("bpName")

	if namespaceName == "" || bpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "bpName", bpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and bpName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("BuildPlane")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, bpName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("BuildPlane not found", "namespace", namespaceName, "name", bpName)
			writeErrorResponse(w, http.StatusNotFound, "BuildPlane not found", services.CodeBuildPlaneNotFound)
			return
		}
		log.Error("Failed to get BuildPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get BuildPlane", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved BuildPlane definition", "namespace", namespaceName, "name", bpName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateBuildPlaneDefinition handles PUT /api/v1/namespaces/{namespaceName}/buildplanes/{bpName}/definition
func (h *Handler) UpdateBuildPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	bpName := r.PathValue("bpName")

	if namespaceName == "" || bpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "bpName", bpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and bpName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "BuildPlane" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be BuildPlane", services.CodeInvalidInput)
		return
	}

	if name != bpName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply BuildPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply BuildPlane: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("BuildPlane applied successfully", "namespace", namespaceName, "name", bpName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteBuildPlaneDefinition handles DELETE /api/v1/namespaces/{namespaceName}/buildplanes/{bpName}/definition
func (h *Handler) DeleteBuildPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	bpName := r.PathValue("bpName")

	if namespaceName == "" || bpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "bpName", bpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and bpName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("BuildPlane")
	obj := buildUnstructuredRef(gvk, namespaceName, bpName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete BuildPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete BuildPlane: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       bpName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("BuildPlane deleted", "namespace", namespaceName, "name", bpName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== ObservabilityPlane Definition Handlers ==========

// GetObservabilityPlaneDefinition handles GET /api/v1/namespaces/{namespaceName}/observabilityplanes/{opName}/definition
func (h *Handler) GetObservabilityPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	opName := r.PathValue("opName")

	if namespaceName == "" || opName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "opName", opName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and opName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("ObservabilityPlane")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, opName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("ObservabilityPlane not found", "namespace", namespaceName, "name", opName)
			writeErrorResponse(w, http.StatusNotFound, "ObservabilityPlane not found", services.CodeNotFound)
			return
		}
		log.Error("Failed to get ObservabilityPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get ObservabilityPlane", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved ObservabilityPlane definition", "namespace", namespaceName, "name", opName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateObservabilityPlaneDefinition handles PUT /api/v1/namespaces/{namespaceName}/observabilityplanes/{opName}/definition
func (h *Handler) UpdateObservabilityPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	opName := r.PathValue("opName")

	if namespaceName == "" || opName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "opName", opName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and opName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "ObservabilityPlane" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be ObservabilityPlane", services.CodeInvalidInput)
		return
	}

	if name != opName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply ObservabilityPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply ObservabilityPlane: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("ObservabilityPlane applied successfully", "namespace", namespaceName, "name", opName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteObservabilityPlaneDefinition handles DELETE /api/v1/namespaces/{namespaceName}/observabilityplanes/{opName}/definition
func (h *Handler) DeleteObservabilityPlaneDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	opName := r.PathValue("opName")

	if namespaceName == "" || opName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "opName", opName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and opName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("ObservabilityPlane")
	obj := buildUnstructuredRef(gvk, namespaceName, opName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete ObservabilityPlane", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete ObservabilityPlane: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       opName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("ObservabilityPlane deleted", "namespace", namespaceName, "name", opName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== DeploymentPipeline Definition Handlers ==========

// GetDeploymentPipelineDefinition handles GET /api/v1/namespaces/{namespaceName}/deployment-pipelines/{dpName}/definition
func (h *Handler) GetDeploymentPipelineDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	dpName := r.PathValue("dpName")

	if namespaceName == "" || dpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "dpName", dpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and dpName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("DeploymentPipeline")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, dpName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("DeploymentPipeline not found", "namespace", namespaceName, "name", dpName)
			writeErrorResponse(w, http.StatusNotFound, "DeploymentPipeline not found", services.CodeDeploymentPipelineNotFound)
			return
		}
		log.Error("Failed to get DeploymentPipeline", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get DeploymentPipeline", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved DeploymentPipeline definition", "namespace", namespaceName, "name", dpName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateDeploymentPipelineDefinition handles PUT /api/v1/namespaces/{namespaceName}/deployment-pipelines/{dpName}/definition
func (h *Handler) UpdateDeploymentPipelineDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	dpName := r.PathValue("dpName")

	if namespaceName == "" || dpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "dpName", dpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and dpName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "DeploymentPipeline" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be DeploymentPipeline", services.CodeInvalidInput)
		return
	}

	if name != dpName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply DeploymentPipeline", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply DeploymentPipeline: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("DeploymentPipeline applied successfully", "namespace", namespaceName, "name", dpName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteDeploymentPipelineDefinition handles DELETE /api/v1/namespaces/{namespaceName}/deployment-pipelines/{dpName}/definition
func (h *Handler) DeleteDeploymentPipelineDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	dpName := r.PathValue("dpName")

	if namespaceName == "" || dpName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "dpName", dpName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and dpName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("DeploymentPipeline")
	obj := buildUnstructuredRef(gvk, namespaceName, dpName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete DeploymentPipeline", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete DeploymentPipeline: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       dpName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("DeploymentPipeline deleted", "namespace", namespaceName, "name", dpName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== Workload Definition Handlers ==========

// GetWorkloadDefinition handles GET /api/v1/namespaces/{namespaceName}/workloads/{workloadName}/definition
func (h *Handler) GetWorkloadDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	workloadName := r.PathValue("workloadName")

	if namespaceName == "" || workloadName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "workloadName", workloadName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and workloadName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Workload")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, workloadName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("Workload not found", "namespace", namespaceName, "name", workloadName)
			writeErrorResponse(w, http.StatusNotFound, "Workload not found", services.CodeWorkloadNotFound)
			return
		}
		log.Error("Failed to get Workload", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get Workload", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved Workload definition", "namespace", namespaceName, "name", workloadName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateWorkloadDefinition handles PUT /api/v1/namespaces/{namespaceName}/workloads/{workloadName}/definition
func (h *Handler) UpdateWorkloadDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	workloadName := r.PathValue("workloadName")

	if namespaceName == "" || workloadName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "workloadName", workloadName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and workloadName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "Workload" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be Workload", services.CodeInvalidInput)
		return
	}

	if name != workloadName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply Workload", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply Workload: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Workload applied successfully", "namespace", namespaceName, "name", workloadName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteWorkloadDefinition handles DELETE /api/v1/namespaces/{namespaceName}/workloads/{workloadName}/definition
func (h *Handler) DeleteWorkloadDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	workloadName := r.PathValue("workloadName")

	if namespaceName == "" || workloadName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "workloadName", workloadName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and workloadName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("Workload")
	obj := buildUnstructuredRef(gvk, namespaceName, workloadName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete Workload", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete Workload: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       workloadName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("Workload deleted", "namespace", namespaceName, "name", workloadName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== SecretReference Definition Handlers ==========

// GetSecretReferenceDefinition handles GET /api/v1/namespaces/{namespaceName}/secret-references/{srName}/definition
func (h *Handler) GetSecretReferenceDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	srName := r.PathValue("srName")

	if namespaceName == "" || srName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "srName", srName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and srName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("SecretReference")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, srName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("SecretReference not found", "namespace", namespaceName, "name", srName)
			writeErrorResponse(w, http.StatusNotFound, "SecretReference not found", services.CodeNotFound)
			return
		}
		log.Error("Failed to get SecretReference", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get SecretReference", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved SecretReference definition", "namespace", namespaceName, "name", srName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateSecretReferenceDefinition handles PUT /api/v1/namespaces/{namespaceName}/secret-references/{srName}/definition
func (h *Handler) UpdateSecretReferenceDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	srName := r.PathValue("srName")

	if namespaceName == "" || srName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "srName", srName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and srName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "SecretReference" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be SecretReference", services.CodeInvalidInput)
		return
	}

	if name != srName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply SecretReference", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply SecretReference: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("SecretReference applied successfully", "namespace", namespaceName, "name", srName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteSecretReferenceDefinition handles DELETE /api/v1/namespaces/{namespaceName}/secret-references/{srName}/definition
func (h *Handler) DeleteSecretReferenceDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	srName := r.PathValue("srName")

	if namespaceName == "" || srName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "srName", srName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and srName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("SecretReference")
	obj := buildUnstructuredRef(gvk, namespaceName, srName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete SecretReference", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete SecretReference: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       srName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("SecretReference deleted", "namespace", namespaceName, "name", srName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== GitSecret Definition Handlers ==========

// GetGitSecretDefinition handles GET /api/v1/namespaces/{namespaceName}/git-secrets/{gsName}/definition
func (h *Handler) GetGitSecretDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	gsName := r.PathValue("gsName")

	if namespaceName == "" || gsName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "gsName", gsName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and gsName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("GitSecret")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, gsName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("GitSecret not found", "namespace", namespaceName, "name", gsName)
			writeErrorResponse(w, http.StatusNotFound, "GitSecret not found", services.CodeGitSecretNotFound)
			return
		}
		log.Error("Failed to get GitSecret", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get GitSecret", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved GitSecret definition", "namespace", namespaceName, "name", gsName)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateGitSecretDefinition handles PUT /api/v1/namespaces/{namespaceName}/git-secrets/{gsName}/definition
func (h *Handler) UpdateGitSecretDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	gsName := r.PathValue("gsName")

	if namespaceName == "" || gsName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "gsName", gsName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and gsName are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "GitSecret" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be GitSecret", services.CodeInvalidInput)
		return
	}

	if name != gsName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply GitSecret", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply GitSecret: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("GitSecret applied successfully", "namespace", namespaceName, "name", gsName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteGitSecretDefinition handles DELETE /api/v1/namespaces/{namespaceName}/git-secrets/{gsName}/definition
func (h *Handler) DeleteGitSecretDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	gsName := r.PathValue("gsName")

	if namespaceName == "" || gsName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "gsName", gsName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and gsName are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("GitSecret")
	obj := buildUnstructuredRef(gvk, namespaceName, gsName)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete GitSecret", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete GitSecret: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       gsName,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("GitSecret deleted", "namespace", namespaceName, "name", gsName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== AuthzRole Definition Handlers ==========

// GetRoleDefinition handles GET /api/v1/namespaces/{namespaceName}/roles/{name}/definition
func (h *Handler) GetRoleDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	name := r.PathValue("name")

	if namespaceName == "" || name == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "name", name)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and name are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("AuthzRole")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, name)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("AuthzRole not found", "namespace", namespaceName, "name", name)
			writeErrorResponse(w, http.StatusNotFound, "AuthzRole not found", services.CodeRoleNotFound)
			return
		}
		log.Error("Failed to get AuthzRole", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get AuthzRole", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved AuthzRole definition", "namespace", namespaceName, "name", name)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateRoleDefinition handles PUT /api/v1/namespaces/{namespaceName}/roles/{name}/definition
func (h *Handler) UpdateRoleDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	roleName := r.PathValue("name")

	if namespaceName == "" || roleName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "name", roleName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and name are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "AuthzRole" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be AuthzRole", services.CodeInvalidInput)
		return
	}

	if name != roleName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply AuthzRole", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply AuthzRole: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("AuthzRole applied successfully", "namespace", namespaceName, "name", roleName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteRoleDefinition handles DELETE /api/v1/namespaces/{namespaceName}/roles/{name}/definition
func (h *Handler) DeleteRoleDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	name := r.PathValue("name")

	if namespaceName == "" || name == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "name", name)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and name are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("AuthzRole")
	obj := buildUnstructuredRef(gvk, namespaceName, name)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete AuthzRole", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete AuthzRole: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("AuthzRole deleted", "namespace", namespaceName, "name", name, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== AuthzRoleBinding Definition Handlers ==========

// GetRoleBindingDefinition handles GET /api/v1/namespaces/{namespaceName}/rolebindings/{name}/definition
func (h *Handler) GetRoleBindingDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	name := r.PathValue("name")

	if namespaceName == "" || name == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "name", name)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and name are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("AuthzRoleBinding")
	obj, err := h.getResourceByGVK(ctx, gvk, namespaceName, name)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("AuthzRoleBinding not found", "namespace", namespaceName, "name", name)
			writeErrorResponse(w, http.StatusNotFound, "AuthzRoleBinding not found", services.CodeRoleBindingNotFound)
			return
		}
		log.Error("Failed to get AuthzRoleBinding", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get AuthzRoleBinding", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved AuthzRoleBinding definition", "namespace", namespaceName, "name", name)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateRoleBindingDefinition handles PUT /api/v1/namespaces/{namespaceName}/rolebindings/{name}/definition
func (h *Handler) UpdateRoleBindingDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	rbName := r.PathValue("name")

	if namespaceName == "" || rbName == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "name", rbName)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and name are required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "AuthzRoleBinding" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be AuthzRoleBinding", services.CodeInvalidInput)
		return
	}

	if name != rbName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}
	unstructuredObj.SetNamespace(namespaceName)

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply AuthzRoleBinding", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply AuthzRoleBinding: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("AuthzRoleBinding applied successfully", "namespace", namespaceName, "name", rbName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteRoleBindingDefinition handles DELETE /api/v1/namespaces/{namespaceName}/rolebindings/{name}/definition
func (h *Handler) DeleteRoleBindingDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	namespaceName := r.PathValue("namespaceName")
	name := r.PathValue("name")

	if namespaceName == "" || name == "" {
		log.Warn("Missing required path parameters", "namespaceName", namespaceName, "name", name)
		writeErrorResponse(w, http.StatusBadRequest, "namespaceName and name are required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("AuthzRoleBinding")
	obj := buildUnstructuredRef(gvk, namespaceName, name)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete AuthzRoleBinding", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete AuthzRoleBinding: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       name,
		Namespace:  namespaceName,
		Operation:  operation,
	}

	log.Info("AuthzRoleBinding deleted", "namespace", namespaceName, "name", name, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== AuthzClusterRole Definition Handlers (Cluster-Scoped) ==========

// GetClusterRoleDefinition handles GET /api/v1/clusterroles/{name}/definition
func (h *Handler) GetClusterRoleDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	name := r.PathValue("name")

	if name == "" {
		log.Warn("Missing required path parameter", "name", name)
		writeErrorResponse(w, http.StatusBadRequest, "name is required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("AuthzClusterRole")
	obj, err := h.getResourceByGVK(ctx, gvk, "", name)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("AuthzClusterRole not found", "name", name)
			writeErrorResponse(w, http.StatusNotFound, "AuthzClusterRole not found", services.CodeRoleNotFound)
			return
		}
		log.Error("Failed to get AuthzClusterRole", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get AuthzClusterRole", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved AuthzClusterRole definition", "name", name)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateClusterRoleDefinition handles PUT /api/v1/clusterroles/{name}/definition
func (h *Handler) UpdateClusterRoleDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	crName := r.PathValue("name")

	if crName == "" {
		log.Warn("Missing required path parameter", "name", crName)
		writeErrorResponse(w, http.StatusBadRequest, "name is required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "AuthzClusterRole" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be AuthzClusterRole", services.CodeInvalidInput)
		return
	}

	if name != crName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply AuthzClusterRole", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply AuthzClusterRole: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Operation:  operation,
	}

	log.Info("AuthzClusterRole applied successfully", "name", crName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteClusterRoleDefinition handles DELETE /api/v1/clusterroles/{name}/definition
func (h *Handler) DeleteClusterRoleDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	name := r.PathValue("name")

	if name == "" {
		log.Warn("Missing required path parameter", "name", name)
		writeErrorResponse(w, http.StatusBadRequest, "name is required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("AuthzClusterRole")
	obj := buildUnstructuredRef(gvk, "", name)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete AuthzClusterRole", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete AuthzClusterRole: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       name,
		Operation:  operation,
	}

	log.Info("AuthzClusterRole deleted", "name", name, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// ========== AuthzClusterRoleBinding Definition Handlers (Cluster-Scoped) ==========

// GetClusterRoleBindingDefinition handles GET /api/v1/clusterrolebindings/{name}/definition
func (h *Handler) GetClusterRoleBindingDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	name := r.PathValue("name")

	if name == "" {
		log.Warn("Missing required path parameter", "name", name)
		writeErrorResponse(w, http.StatusBadRequest, "name is required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("AuthzClusterRoleBinding")
	obj, err := h.getResourceByGVK(ctx, gvk, "", name)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Warn("AuthzClusterRoleBinding not found", "name", name)
			writeErrorResponse(w, http.StatusNotFound, "AuthzClusterRoleBinding not found", services.CodeRoleBindingNotFound)
			return
		}
		log.Error("Failed to get AuthzClusterRoleBinding", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to get AuthzClusterRoleBinding", services.CodeInternalError)
		return
	}

	log.Debug("Retrieved AuthzClusterRoleBinding definition", "name", name)
	writeSuccessResponse(w, http.StatusOK, obj.Object)
}

// UpdateClusterRoleBindingDefinition handles PUT /api/v1/clusterrolebindings/{name}/definition
func (h *Handler) UpdateClusterRoleBindingDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	crbName := r.PathValue("name")

	if crbName == "" {
		log.Warn("Missing required path parameter", "name", crbName)
		writeErrorResponse(w, http.StatusBadRequest, "name is required", services.CodeInvalidInput)
		return
	}

	var resourceObj map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&resourceObj); err != nil {
		log.Error("Failed to decode request body", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", services.CodeInvalidInput)
		return
	}

	kind, apiVersion, name, err := validateResourceRequest(resourceObj)
	if err != nil {
		writeErrorResponse(w, http.StatusBadRequest, err.Error(), services.CodeInvalidInput)
		return
	}

	if kind != "AuthzClusterRoleBinding" {
		writeErrorResponse(w, http.StatusBadRequest, "Kind must be AuthzClusterRoleBinding", services.CodeInvalidInput)
		return
	}

	if name != crbName {
		writeErrorResponse(w, http.StatusBadRequest, "Resource name does not match URL", services.CodeInvalidInput)
		return
	}

	unstructuredObj := &unstructured.Unstructured{Object: resourceObj}

	if err := h.handleResourceNamespace(unstructuredObj, apiVersion, kind); err != nil {
		log.Error("Failed to handle resource namespace", "error", err)
		writeErrorResponse(w, http.StatusBadRequest, "Failed to handle resource namespace: "+err.Error(), services.CodeInvalidInput)
		return
	}

	operation, err := h.applyToKubernetes(ctx, unstructuredObj)
	if err != nil {
		log.Error("Failed to apply AuthzClusterRoleBinding", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to apply AuthzClusterRoleBinding: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Operation:  operation,
	}

	log.Info("AuthzClusterRoleBinding applied successfully", "name", crbName, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}

// DeleteClusterRoleBindingDefinition handles DELETE /api/v1/clusterrolebindings/{name}/definition
func (h *Handler) DeleteClusterRoleBindingDefinition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.GetLogger(ctx)

	name := r.PathValue("name")

	if name == "" {
		log.Warn("Missing required path parameter", "name", name)
		writeErrorResponse(w, http.StatusBadRequest, "name is required", services.CodeInvalidInput)
		return
	}

	gvk := openChoreoGVK("AuthzClusterRoleBinding")
	obj := buildUnstructuredRef(gvk, "", name)

	operation, err := h.deleteFromKubernetes(ctx, obj)
	if err != nil {
		log.Error("Failed to delete AuthzClusterRoleBinding", "error", err)
		writeErrorResponse(w, http.StatusInternalServerError, "Failed to delete AuthzClusterRoleBinding: "+err.Error(), services.CodeInternalError)
		return
	}

	response := ResourceCRUDResponse{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       name,
		Operation:  operation,
	}

	log.Info("AuthzClusterRoleBinding deleted", "name", name, "operation", operation)
	writeSuccessResponse(w, http.StatusOK, response)
}
