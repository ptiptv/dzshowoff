package shjs

import (
	"github.com/cratonica/embed"
)

var Files embed.ResourceMap

func init() {
	var err error
	Files, err = embed.Unpack(Resources)
	if err != nil {
		panic(err)
	}
}
