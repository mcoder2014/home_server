package main

import (
	"github.com/mcoder2014/home_server/route"
)

func main() {
	r := route.InitRoute()
	// listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
	if err :=r.Run(":8888"); err!=nil {
		panic(err)
	}
}