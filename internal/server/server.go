package server

import (
	"net/http"

	"github.com/ditwrd/pavedway/frontend"
	"github.com/ditwrd/pavedway/internal/store"
	"github.com/labstack/echo/v5"
)

func New(q *store.Queries) *echo.Echo {
	e := echo.New()

	dist, err := frontend.Dist()
	if err != nil {
		panic(err)
	}

	e.FileFS("/", "index.html", dist)
	e.StaticFS("/", dist)

	e.GET("/healthz", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, "Ok")
	})
	h := &handlers{q: q}
	api := e.Group("/api/v1")
	api.POST("/bootstrap", h.bootstrap)
	api.POST("/entities", h.createEntity)
	api.GET("/entities/:kind/:namespace/:name", h.getEntity)
	api.PUT("/entities/:kind/:namespace/:name", h.updateEntity)
	api.DELETE("/entities/:kind/:namespace/:name", h.deleteEntity)

	return e
}
