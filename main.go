package main

import (
	"github.com/mcoder2014/home_server/route"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})

	r := route.InitRoute()
	// listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
	if err :=r.Run(":8888"); err!=nil {
		panic(err)
	}
}