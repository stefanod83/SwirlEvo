package api

import (
	"time"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

// ImageHandler encapsulates image related handlers.
type ImageHandler struct {
	Search web.HandlerFunc `path:"/search" auth:"image.view" desc:"search images"`
	Find   web.HandlerFunc `path:"/find" auth:"image.view" desc:"find image by id"`
	Delete web.HandlerFunc `path:"/delete" method:"post" auth:"image.delete" desc:"delete image"`
	Prune  web.HandlerFunc `path:"/prune" method:"post" auth:"image.delete" desc:"delete unused images"`
	Tag    web.HandlerFunc `path:"/tag" method:"post" auth:"image.edit" desc:"add a new tag to an existing image"`
	Push   web.HandlerFunc `path:"/push" method:"post" auth:"image.push" desc:"push an image ref to a registry"`
}

// NewImage creates an instance of ImageHandler
func NewImage(b biz.ImageBiz) *ImageHandler {
	return &ImageHandler{
		Search: imageSearch(b),
		Find:   imageFind(b),
		Delete: imageDelete(b),
		Prune:  imagePrune(b),
		Tag:    imageTag(b),
		Push:   imagePush(b),
	}
}

func imageSearch(b biz.ImageBiz) web.HandlerFunc {
	type Args struct {
		Node      string `json:"node" bind:"node"`
		Name      string `json:"name" bind:"name"`
		PageIndex int    `json:"pageIndex" bind:"pageIndex"`
		PageSize  int    `json:"pageSize" bind:"pageSize"`
	}

	return func(c web.Context) (err error) {
		var (
			args   = &Args{}
			images []*biz.Image
			total  int
		)

		if err = c.Bind(args); err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			images, total, err = b.Search(ctx, args.Node, args.Name, args.PageIndex, args.PageSize)
		}

		if err != nil {
			return
		}

		return success(c, data.Map{
			"items": images,
			"total": total,
		})
	}
}

func imageFind(b biz.ImageBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		node := c.Query("node")
		id := c.Query("id")
		image, raw, err := b.Find(ctx, node, id)
		if err != nil {
			return err
		}
		return success(c, data.Map{"image": image, "raw": raw})
	}
}

func imageDelete(b biz.ImageBiz) web.HandlerFunc {
	type Args struct {
		Node  string `json:"node"`
		ID    string `json:"id"`
		Force bool   `json:"force"`
	}

	return func(c web.Context) (err error) {
		args := &Args{}
		if err = c.Bind(args); err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			err = b.Delete(ctx, args.Node, args.ID, args.Force, c.User())
		}
		return ajax(c, err)
	}
}

func imagePrune(b biz.ImageBiz) web.HandlerFunc {
	type Args struct {
		Node string `json:"node"`
	}

	return func(c web.Context) (err error) {
		args := &Args{}
		if err = c.Bind(args); err != nil {
			return err
		}

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		count, size, err := b.Prune(ctx, args.Node, c.User())
		if err != nil {
			return err
		}

		return success(c, data.Map{
			"count": count,
			"size":  size,
		})
	}
}

func imageTag(b biz.ImageBiz) web.HandlerFunc {
	type Args struct {
		Node   string `json:"node"`
		Source string `json:"source"`
		Target string `json:"target"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return ajax(c, err)
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Tag(ctx, args.Node, args.Source, args.Target, c.User()))
	}
}

func imagePush(b biz.ImageBiz) web.HandlerFunc {
	type Args struct {
		Node       string `json:"node"`
		Ref        string `json:"ref"`
		RegistryID string `json:"registryId"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return ajax(c, err)
		}
		// Push can take minutes for large images; override the default
		// request timeout with a generous one so the reverse proxy
		// doesn't cut us off.
		ctx, cancel := misc.Context(10 * time.Minute)
		defer cancel()
		return ajax(c, b.Push(ctx, args.Node, args.Ref, args.RegistryID, c.User()))
	}
}
