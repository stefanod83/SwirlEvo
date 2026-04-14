package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// ContainerHandler encapsulates container related handlers.
type ContainerHandler struct {
	Search    web.HandlerFunc `path:"/search" auth:"container.view" desc:"search containers"`
	Find      web.HandlerFunc `path:"/find" auth:"container.view" desc:"find container by name"`
	Delete    web.HandlerFunc `path:"/delete" method:"post" auth:"container.delete" desc:"delete container"`
	FetchLogs web.HandlerFunc `path:"/fetch-logs" auth:"container.logs" desc:"fetch logs of container"`
	Connect   web.HandlerFunc `path:"/connect" auth:"container.execute" desc:"connect to a running container"`
	Prune     web.HandlerFunc `path:"/prune" method:"post" auth:"container.delete" desc:"delete unused containers"`
	Start     web.HandlerFunc `path:"/start" method:"post" auth:"container.edit" desc:"start container"`
	Stop      web.HandlerFunc `path:"/stop" method:"post" auth:"container.edit" desc:"stop container"`
	Restart   web.HandlerFunc `path:"/restart" method:"post" auth:"container.edit" desc:"restart container"`
	Kill      web.HandlerFunc `path:"/kill" method:"post" auth:"container.edit" desc:"kill container"`
	Pause     web.HandlerFunc `path:"/pause" method:"post" auth:"container.edit" desc:"pause container"`
	Unpause   web.HandlerFunc `path:"/unpause" method:"post" auth:"container.edit" desc:"unpause container"`
	Rename    web.HandlerFunc `path:"/rename" method:"post" auth:"container.edit" desc:"rename container"`
	Stats     web.HandlerFunc `path:"/stats" auth:"container.view" desc:"container stats snapshot"`
}

// NewContainer creates an instance of ContainerHandler
func NewContainer(b biz.ContainerBiz) *ContainerHandler {
	return &ContainerHandler{
		Search:    containerSearch(b),
		Find:      containerFind(b),
		Delete:    containerDelete(b),
		FetchLogs: containerFetchLogs(b),
		Connect:   containerConnect(b),
		Prune:     containerPrune(b),
		Start:     containerStart(b),
		Stop:      containerStop(b),
		Restart:   containerRestart(b),
		Kill:      containerKill(b),
		Pause:     containerPause(b),
		Unpause:   containerUnpause(b),
		Rename:    containerRename(b),
		Stats:     containerStats(b),
	}
}

func containerSearch(b biz.ContainerBiz) web.HandlerFunc {
	type Args struct {
		Node      string `json:"node" bind:"node"`
		Name      string `json:"name" bind:"name"`
		Status    string `json:"status" bind:"status"`
		Project   string `json:"project" bind:"project"`
		PageIndex int    `json:"pageIndex" bind:"pageIndex"`
		PageSize  int    `json:"pageSize" bind:"pageSize"`
	}

	return func(c web.Context) (err error) {
		var (
			args       = &Args{}
			containers []*biz.Container
			total      int
		)

		if err = c.Bind(args); err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			containers, total, err = b.Search(ctx, args.Node, args.Name, args.Status, args.Project, args.PageIndex, args.PageSize)
		}

		if err != nil {
			return
		}

		return success(c, data.Map{
			"items": containers,
			"total": total,
		})
	}
}

func containerFind(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		node := c.Query("node")
		id := c.Query("id")
		container, raw, err := b.Find(ctx, node, id)
		if err != nil {
			return err
		} else if container == nil {
			return web.NewError(http.StatusNotFound)
		}
		return success(c, data.Map{"container": container, "raw": raw})
	}
}

func containerDelete(b biz.ContainerBiz) web.HandlerFunc {
	type Args struct {
		Node string `json:"node"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	return func(c web.Context) (err error) {
		args := &Args{}
		if err = c.Bind(args); err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			err = b.Delete(ctx, args.Node, args.ID, args.Name, c.User())
		}
		return ajax(c, err)
	}
}

func containerFetchLogs(b biz.ContainerBiz) web.HandlerFunc {
	type Args struct {
		Node       string `json:"node" bind:"node"`
		ID         string `json:"id" bind:"id"`
		Lines      int    `json:"lines" bind:"lines"`
		Timestamps bool   `json:"timestamps" bind:"timestamps"`
	}

	return func(c web.Context) (err error) {
		var (
			args           = &Args{}
			stdout, stderr string
		)
		if err = c.Bind(args); err == nil {
			ctx, cancel := misc.Context(defaultTimeout)
			defer cancel()

			stdout, stderr, err = b.FetchLogs(ctx, args.Node, args.ID, args.Lines, args.Timestamps)
		}
		if err != nil {
			return err
		}
		return success(c, data.Map{"stdout": stdout, "stderr": stderr})
	}
}

func containerConnect(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		var (
			node = c.Query("node")
			id   = c.Query("id")
			cmd  = c.Query("cmd")
		)

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		container, _, err := b.Find(ctx, node, id)
		if err != nil {
			return err
		} else if container == nil {
			return web.NewError(http.StatusNotFound)
		}

		conn, _, _, err := ws.UpgradeHTTP(c.Request(), c.Response())
		if err != nil {
			return err
		}

		idResp, err := b.ExecCreate(ctx, node, id, cmd)
		if err != nil {
			return err
		}

		resp, err := b.ExecAttach(ctx, node, idResp.ID)
		if err != nil {
			return err
		}

		err = b.ExecStart(ctx, node, idResp.ID)
		if err != nil {
			return err
		}

		var (
			closed   = false
			logger   = log.Get("container")
			disposer = func() {
				if !closed {
					closed = true
					_ = conn.Close()
					resp.Close()
				}
			}
		)

		// input
		go func() {
			defer disposer()

			var (
				msg []byte
				op  ws.OpCode
			)

			for {
				msg, op, err = wsutil.ReadClientData(conn)
				if err != nil {
					if !closed {
						logger.Error("failed to read data from client: ", err)
					}
					break
				}

				if op == ws.OpClose {
					break
				}

				_, err = resp.Conn.Write(msg)
				if err != nil {
					logger.Error("failed to write data to container: ", err)
					break
				}
			}
		}()

		// output
		go func() {
			defer disposer()

			var (
				n   int
				buf = make([]byte, 1024)
			)

			for {
				n, err = resp.Reader.Read(buf)
				if err == io.EOF {
					break
				} else if err != nil {
					logger.Error("failed to read data from container: ", err)
					break
				}

				err = wsutil.WriteServerMessage(conn, ws.OpText, buf[:n])
				if err != nil {
					logger.Error("failed to write data to client: ", err)
					break
				}
			}
		}()
		return nil
	}
}

func containerPrune(b biz.ContainerBiz) web.HandlerFunc {
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

type containerActionArgs struct {
	Node    string `json:"node"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Timeout int    `json:"timeout"`
	Signal  string `json:"signal"`
	NewName string `json:"newName"`
}

func containerStart(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &containerActionArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Start(ctx, args.Node, args.ID, args.Name, c.User()))
	}
}

func containerStop(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &containerActionArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Stop(ctx, args.Node, args.ID, args.Name, args.Timeout, c.User()))
	}
}

func containerRestart(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &containerActionArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Restart(ctx, args.Node, args.ID, args.Name, args.Timeout, c.User()))
	}
}

func containerKill(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &containerActionArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Kill(ctx, args.Node, args.ID, args.Name, args.Signal, c.User()))
	}
}

func containerPause(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &containerActionArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Pause(ctx, args.Node, args.ID, args.Name, c.User()))
	}
}

func containerUnpause(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &containerActionArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Unpause(ctx, args.Node, args.ID, args.Name, c.User()))
	}
}

func containerRename(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		args := &containerActionArgs{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		return ajax(c, b.Rename(ctx, args.Node, args.ID, args.Name, args.NewName, c.User()))
	}
}

func containerStats(b biz.ContainerBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		node := c.Query("node")
		id := c.Query("id")
		raw, err := b.Stats(ctx, node, id)
		if err != nil {
			return err
		}
		var stats data.Map
		if err := json.Unmarshal(raw, &stats); err != nil {
			return err
		}
		return success(c, stats)
	}
}
