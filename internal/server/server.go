package server

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

func New() *echo.Echo {
	e := echo.New()

	e.GET("/healthz", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, "Ok")
	})

	return e
}
