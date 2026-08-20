package server

import (
	"net/http"

	"github.com/ditwrd/pavedway/frontend"
	"github.com/labstack/echo/v5"
)

func New() *echo.Echo {
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

	return e
}
