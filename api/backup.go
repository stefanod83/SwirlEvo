package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

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
	KeyStatus      web.HandlerFunc `path:"/key-status" auth:"backup.view" desc:"current key fingerprint and aggregate verification summary"`
	Create         web.HandlerFunc `path:"/create" method:"post" auth:"backup.edit" desc:"create a manual backup"`
	Delete         web.HandlerFunc `path:"/delete" method:"post" auth:"backup.delete" desc:"delete a backup"`
	Download       web.HandlerFunc `path:"/download" method:"post" auth:"backup.download" desc:"download a backup archive"`
	Restore        web.HandlerFunc `path:"/restore" method:"post" auth:"backup.restore" desc:"restore from a stored backup"`
	Verify         web.HandlerFunc `path:"/verify" method:"post" auth:"backup.view" desc:"re-probe one backup against the current master key"`
	Recover        web.HandlerFunc `path:"/recover" method:"post" auth:"backup.recover" desc:"re-encrypt a backup with the current key using the old passphrase"`
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
		KeyStatus:      backupKeyStatus(b),
		Create:         backupCreate(b),
		Delete:         backupDelete(b),
		Download:       backupDownload(b),
		Restore:        backupRestore(b),
		Verify:         backupVerify(b),
		Recover:        backupRecover(b),
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
		// Surface the source ("env" / "cache" / "vault" / "") and the
		// real lookup error so operators can diagnose mismatched
		// path/field config without having to read server logs.
		ok, source, err := b.KeyStatusDiag()
		out := map[string]interface{}{
			"keyConfigured": ok,
			"keySource":     source,
		}
		if err != nil {
			out["keyError"] = err.Error()
		}
		return success(c, out)
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

func backupKeyStatus(b biz.BackupBiz) web.HandlerFunc {
	return func(c web.Context) error {
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()
		summary := b.VerifyAll(ctx)
		return success(c, summary)
	}
}

func backupVerify(b biz.BackupBiz) web.HandlerFunc {
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
		rec, err := b.Verify(ctx, args.ID)
		if err != nil {
			return err
		}
		return success(c, rec)
	}
}

func backupRecover(b biz.BackupBiz) web.HandlerFunc {
	type Args struct {
		ID            string `json:"id"`
		OldPassphrase string `json:"oldPassphrase"`
	}
	return func(c web.Context) error {
		args := &Args{}
		if err := c.Bind(args); err != nil {
			return err
		}
		ctx, cancel := misc.Context(5 * defaultTimeout)
		defer cancel()
		rec, err := b.Recover(ctx, args.ID, args.OldPassphrase, c.User())
		if err != nil {
			// Map well-known errors to actionable HTTP statuses so the UI
			// can show a precise message instead of a generic 500.
			switch {
			case errors.Is(err, os.ErrNotExist):
				return web.NewError(http.StatusGone, "backup file is missing on disk")
			case strings.Contains(err.Error(), "decryption failed"):
				return web.NewError(http.StatusUnauthorized, "the supplied passphrase did not decrypt the archive — verify it matches the SWIRL_BACKUP_KEY in effect when the backup was created")
			case strings.Contains(err.Error(), "SWIRL_BACKUP_KEY is not configured"):
				return web.NewError(http.StatusPreconditionFailed, "no master key configured — set SWIRL_BACKUP_KEY or configure Vault before recovering")
			case strings.Contains(err.Error(), "passphrase must be at least"):
				return web.NewError(http.StatusBadRequest, err.Error())
			}
			return err
		}
		return success(c, rec)
	}
}

