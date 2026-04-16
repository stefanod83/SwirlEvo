package api

import (
	"io"

	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// BackupHandler encapsulates backup management endpoints.
type BackupHandler struct {
	Search         web.HandlerFunc `path:"/search" auth:"backup.view" desc:"list backups"`
	Find           web.HandlerFunc `path:"/find" auth:"backup.view" desc:"get backup metadata"`
	Status         web.HandlerFunc `path:"/status" auth:"backup.view" desc:"check backup subsystem status"`
	Create         web.HandlerFunc `path:"/create" method:"post" auth:"backup.edit" desc:"create a manual backup"`
	Delete         web.HandlerFunc `path:"/delete" method:"post" auth:"backup.delete" desc:"delete a backup"`
	Download       web.HandlerFunc `path:"/download" method:"post" auth:"backup.download" desc:"download a backup archive"`
	Restore        web.HandlerFunc `path:"/restore" method:"post" auth:"backup.restore" desc:"restore from a stored backup"`
	Preview        web.HandlerFunc `path:"/preview" method:"post" auth:"backup.restore" desc:"preview an uploaded backup"`
	Upload         web.HandlerFunc `path:"/upload" method:"post" auth:"backup.restore" desc:"restore from an uploaded backup file"`
	Schedules      web.HandlerFunc `path:"/schedules" auth:"backup.view" desc:"list backup schedules"`
	SaveSchedule   web.HandlerFunc `path:"/schedule/save" method:"post" auth:"backup.edit" desc:"create or update a backup schedule"`
	DeleteSchedule web.HandlerFunc `path:"/schedule/delete" method:"post" auth:"backup.edit" desc:"delete a backup schedule"`
}

// NewBackup creates an instance of BackupHandler.
func NewBackup(b biz.BackupBiz) *BackupHandler {
	return &BackupHandler{
		Search:         backupSearch(b),
		Find:           backupFind(b),
		Status:         backupStatus(b),
		Create:         backupCreate(b),
		Delete:         backupDelete(b),
		Download:       backupDownload(b),
		Restore:        backupRestore(b),
		Preview:        backupPreview(b),
		Upload:         backupUpload(b),
		Schedules:      backupSchedules(b),
		SaveSchedule:   backupSaveSchedule(b),
		DeleteSchedule: backupDeleteSchedule(b),
	}
}

func backupSearch(b biz.BackupBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		records, err := b.List(ctx)
		if err != nil {
			return err
		}
		return success(c, records)
	}
}

func backupFind(b biz.BackupBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		id := c.Query("id")
		rec, err := b.Find(ctx, id)
		if err != nil {
			return err
		}
		return success(c, rec)
	}
}

func backupStatus(b biz.BackupBiz) web.HandlerFunc {
	return func(c web.Context) error {
		return success(c, map[string]interface{}{
			"keyConfigured": b.KeyConfigured(),
		})
	}
}

func backupCreate(b biz.BackupBiz) web.HandlerFunc {
	type Args struct {
		Source string `json:"source"`
	}
	return func(c web.Context) error {
		args := &Args{}
		_ = c.Bind(args)

		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()

		rec, err := b.Create(ctx, args.Source, c.User())
		if err != nil {
			return err
		}
		return success(c, rec)
	}
}

func backupDelete(b biz.BackupBiz) web.HandlerFunc {
	type Args struct {
		ID string `json:"id"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		err := b.Delete(ctx, args.ID, c.User())
		return ajax(c, err)
	}
}

func backupDownload(b biz.BackupBiz) web.HandlerFunc {
	type Args struct {
		ID       string `json:"id"`
		Mode     string `json:"mode"` // "raw" or "portable"
		Password string `json:"password"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()

		filename, payload, err := b.Open(ctx, args.ID, args.Mode, args.Password, c.User())
		if err != nil {
			return err
		}
		return c.Data(payload, web.ContentDisposition{
			Type: web.DispositionAttachment,
			Name: filename,
		})
	}
}

func backupRestore(b biz.BackupBiz) web.HandlerFunc {
	type Args struct {
		ID         string   `json:"id"`
		Components []string `json:"components"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()

		counts, err := b.Restore(ctx, args.ID, args.Components, c.User())
		if err != nil {
			return err
		}
		return success(c, counts)
	}
}

func backupPreview(b biz.BackupBiz) web.HandlerFunc {
	return func(c web.Context) error {
		file, _, err := c.File("content")
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		password := c.Form("password")

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		manifest, err := b.PreviewUpload(ctx, raw, password)
		if err != nil {
			return err
		}
		return success(c, manifest)
	}
}

func backupUpload(b biz.BackupBiz) web.HandlerFunc {
	return func(c web.Context) error {
		file, _, err := c.File("content")
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		password := c.Form("password")
		components := c.Request().Form["components"]

		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()

		counts, err := b.RestoreUpload(ctx, raw, password, components, c.User())
		if err != nil {
			return err
		}
		return success(c, counts)
	}
}

func backupSchedules(b biz.BackupBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		schedules, err := b.Schedules(ctx)
		if err != nil {
			return err
		}
		return success(c, schedules)
	}
}

func backupSaveSchedule(b biz.BackupBiz) web.HandlerFunc {
	return func(c web.Context) error {
		s := &dao.BackupSchedule{}
		if err := c.Bind(s, true); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		err := b.SaveSchedule(ctx, s, c.User())
		return ajax(c, err)
	}
}

func backupDeleteSchedule(b biz.BackupBiz) web.HandlerFunc {
	type Args struct {
		ID string `json:"id"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		err := b.DeleteSchedule(ctx, args.ID, c.User())
		return ajax(c, err)
	}
}

