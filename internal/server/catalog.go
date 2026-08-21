package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ditwrd/pavedway/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"
)

type handlers struct {
	q *store.Queries
}

func errorResponse(err error) map[string]string {
	return map[string]string{"error": err.Error()}
}

type bootstrapRequest struct {
	Name string `json:"name"`
}

func (h *handlers) bootstrap(c *echo.Context) error {
	var req bootstrapRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse(err))
	}

	org, err := h.q.BootstrapOrganization(c.Request().Context(), req.Name)
	if err != nil {
		if errors.Is(err, store.ErrOrganizationExists) {
			return c.JSON(http.StatusConflict, errorResponse(err))
		}
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}
	return c.JSON(http.StatusCreated, org)
}

type entityRequest struct {
	Kind      string          `json:"kind"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Metadata  json.RawMessage `json:"metadata"`
	Spec      json.RawMessage `json:"spec"`
}
type entityResponse struct {
	Kind      string          `json:"kind"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Metadata  json.RawMessage `json:"metadata"`
	Spec      json.RawMessage `json:"spec"`
}

func toEntityResponse(e store.Entity) entityResponse {
	return entityResponse{
		Kind:      e.Kind,
		Namespace: e.Namespace,
		Name:      e.Name,
		Metadata:  e.Metadata,
		Spec:      e.Spec,
	}
}

func (h *handlers) createEntity(c *echo.Context) error {
	ctx := c.Request().Context()

	var req entityRequest
	err := c.Bind(&req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse(err))
	}

	org, err := h.q.GetOrganization(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	entity, err := h.q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID:     org.ID,
		Kind:      req.Kind,
		Namespace: req.Namespace,
		Name:      req.Name,
		Metadata:  req.Metadata,
		Spec:      req.Spec,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}
	return c.JSON(http.StatusCreated, toEntityResponse(entity))
}

func (h *handlers) getEntity(c *echo.Context) error {
	ctx := c.Request().Context()

	org, err := h.q.GetOrganization(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	entity, err := h.q.GetEntity(ctx, store.GetEntityParams{
		OrgID:     org.ID,
		Kind:      c.Param("kind"),
		Namespace: c.Param("namespace"),
		Name:      c.Param("name"),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.NoContent(http.StatusNotFound)
		}
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	return c.JSON(http.StatusOK, toEntityResponse(entity))
}

func (h *handlers) updateEntity(c *echo.Context) error {
	ctx := c.Request().Context()

	var req entityRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse(err))
	}

	org, err := h.q.GetOrganization(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	entity, err := h.q.UpdateEntity(ctx, store.UpdateEntityParams{
		OrgID:     org.ID,
		Kind:      c.Param("kind"),
		Namespace: c.Param("namespace"),
		Name:      c.Param("name"),
		Metadata:  req.Metadata,
		Spec:      req.Spec,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c.NoContent(http.StatusNotFound)
		}
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}
	return c.JSON(http.StatusOK, toEntityResponse(entity))
}

func (h *handlers) deleteEntity(c *echo.Context) error {
	ctx := c.Request().Context()

	org, err := h.q.GetOrganization(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	rows, err := h.q.DeleteEntity(ctx, store.DeleteEntityParams{
		OrgID:     org.ID,
		Kind:      c.Param("kind"),
		Namespace: c.Param("namespace"),
		Name:      c.Param("name"),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}
	if rows == 0 {
		return c.NoContent(http.StatusNotFound)
	}
	return c.NoContent(http.StatusNoContent)
}
