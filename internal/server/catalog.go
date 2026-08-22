package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ditwrd/pavedway/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"
)

// errorResponse returns a generic client-facing error and logs the raw
// cause server-side. Internal errors — DB failures, oauth2 exchanges,
// token verification — must never reach the client verbatim
// (golang-security: "Returning detailed errors" lets attackers map the
// system); the specific message is for the operator's logs.
func errorResponse(err error) map[string]string {
	slog.Error("api error", "err", err)
	return map[string]string{"error": "internal error"}
}

// errorMessage returns a deliberately client-safe message. Use only for
// errors that are already generic and user-facing (validation failures,
// sentinel states); never pass a raw internal error here.
func errorMessage(msg string) map[string]string {
	return map[string]string{"error": msg}
}

type bootstrapRequest struct {
	Name string `json:"name"`
}

func (h *handlers) bootstrap(c *echo.Context) error {
	var req bootstrapRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorMessage(err.Error()))
	}

	org, err := h.q.BootstrapOrganization(c.Request().Context(), req.Name)
	if err != nil {
		if errors.Is(err, store.ErrOrganizationExists) {
			return c.JSON(http.StatusConflict, errorMessage(err.Error()))
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
		return c.JSON(http.StatusBadRequest, errorMessage(err.Error()))
	}

	orgID, err := h.orgID(c)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	entity, err := h.q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID:     orgID,
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

	orgID, err := h.orgID(c)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	entity, err := h.q.GetEntity(ctx, store.GetEntityParams{
		OrgID:     orgID,
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
		return c.JSON(http.StatusBadRequest, errorMessage(err.Error()))
	}

	orgID, err := h.orgID(c)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	entity, err := h.q.UpdateEntity(ctx, store.UpdateEntityParams{
		OrgID:     orgID,
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

	orgID, err := h.orgID(c)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse(err))
	}

	rows, err := h.q.DeleteEntity(ctx, store.DeleteEntityParams{
		OrgID:     orgID,
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
