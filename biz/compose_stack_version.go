package biz

import (
	"context"
	"errors"
	"fmt"

	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// defaultStackVersionRetention caps the number of history entries kept per
// stack. Older revisions are pruned in a best-effort pass after every
// snapshot. Twenty is enough for typical edit-train rhythms (one revision
// per day for a month) without bloating BoltDB significantly.
const defaultStackVersionRetention = 20

// snapshotIfChanged captures a point-in-time copy of the PRE-save state of a
// stack so the operator can diff/restore it later. Called from Save() before
// the mutating ComposeStackUpdate. No-op on create (no prior state exists)
// and on unchanged saves (same Content AND EnvFile).
//
// Failures here are NOT propagated: a snapshot is best-effort. Losing the
// version history is strictly better than refusing to save because the
// snapshot bucket is momentarily unavailable.
func (b *composeStackBiz) snapshotIfChanged(ctx context.Context, next *dao.ComposeStack, reason string, user web.User) {
	prev, err := b.di.ComposeStackGet(ctx, next.ID)
	if err != nil || prev == nil {
		return
	}
	if prev.Content == next.Content && prev.EnvFile == next.EnvFile {
		return
	}
	revision := 1
	existing, lErr := b.di.ComposeStackVersionList(ctx, next.ID, 1)
	if lErr == nil && len(existing) > 0 {
		revision = existing[0].Revision + 1
	}
	v := &dao.ComposeStackVersion{
		ID:        createId(),
		StackID:   next.ID,
		Revision:  revision,
		Content:   prev.Content,
		EnvFile:   prev.EnvFile,
		Reason:    reason,
		CreatedAt: now(),
		CreatedBy: newOperator(user),
	}
	if cErr := b.di.ComposeStackVersionCreate(ctx, v); cErr != nil {
		// Soft-log via event system would risk a cascade; rely on the
		// biz logger once one is wired up. For now we swallow — the
		// operator still gets the save they asked for.
		return
	}
	_ = b.di.ComposeStackVersionPrune(ctx, next.ID, defaultStackVersionRetention)
}

// ListVersions returns snapshot metadata for a stack, newest-first. The
// Content/EnvFile fields are stripped from list responses to keep the
// payload cheap — callers that need the body ask for GetVersion.
func (b *composeStackBiz) ListVersions(ctx context.Context, stackID string) ([]*dao.ComposeStackVersion, error) {
	if stackID == "" {
		return nil, errors.New("stackId is required")
	}
	items, err := b.di.ComposeStackVersionList(ctx, stackID, 0)
	if err != nil {
		return nil, err
	}
	// Defensive copy + strip bodies. We don't want a later modification
	// to the cached slice to leak bytes into the HTTP response.
	out := make([]*dao.ComposeStackVersion, 0, len(items))
	for _, v := range items {
		out = append(out, &dao.ComposeStackVersion{
			ID:        v.ID,
			StackID:   v.StackID,
			Revision:  v.Revision,
			Reason:    v.Reason,
			CreatedAt: v.CreatedAt,
			CreatedBy: v.CreatedBy,
		})
	}
	return out, nil
}

// GetVersion fetches a single version with its full Content + EnvFile so the
// UI can render a diff view against the current stack.
func (b *composeStackBiz) GetVersion(ctx context.Context, versionID string) (*dao.ComposeStackVersion, error) {
	if versionID == "" {
		return nil, errors.New("versionId is required")
	}
	v, err := b.di.ComposeStackVersionGet(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, misc.Error(misc.ErrStackNotFound, fmt.Errorf("stack version %q not found", versionID))
	}
	return v, nil
}

// ParseAddons exposes the extract reverse-parser so the API layer can
// surface it to the editor. Thin wrapper that lets the biz interface stay
// the single injection point for everything compose-stack related.
func (b *composeStackBiz) ParseAddons(content string) (*AddonsConfig, error) {
	return extractAddonConfig(content)
}

// RestoreVersion replaces the current stack's Content + EnvFile with those of
// a previous snapshot. A new snapshot is created BEFORE the overwrite so the
// restore itself is reversible — reason="restore:rev<N>" so the UI can tell
// restores apart from plain saves in the History dropdown.
func (b *composeStackBiz) RestoreVersion(ctx context.Context, stackID, versionID string, user web.User) error {
	if stackID == "" || versionID == "" {
		return errors.New("stackId and versionId are required")
	}
	version, err := b.di.ComposeStackVersionGet(ctx, versionID)
	if err != nil {
		return err
	}
	if version == nil || version.StackID != stackID {
		return misc.Error(misc.ErrStackNotFound, fmt.Errorf("stack version %q not found for stack %q", versionID, stackID))
	}
	current, err := b.di.ComposeStackGet(ctx, stackID)
	if err != nil {
		return err
	}
	if current == nil {
		return misc.Error(misc.ErrStackNotFound, fmt.Errorf("stack %q not found", stackID))
	}

	// No-op when current state already matches — restoring to the same
	// content just pollutes the history with empty snapshots.
	if current.Content == version.Content && current.EnvFile == version.EnvFile {
		return nil
	}

	// Take a snapshot of the CURRENT state before we overwrite it.
	// snapshotIfChanged reads `prev` from DB (== current) and compares
	// against the `next` we pass (== target version), so the bytes that
	// land in the snapshot are the ones the user is about to lose.
	synthetic := &dao.ComposeStack{
		ID:      current.ID,
		Content: version.Content,
		EnvFile: version.EnvFile,
	}
	reason := fmt.Sprintf("restore:rev%d", version.Revision)
	b.snapshotIfChanged(ctx, synthetic, reason, user)

	// Apply the restore: only Content + EnvFile are replaced. Status /
	// host / name are untouched so a restore never moves or re-runs the
	// containers.
	current.Content = version.Content
	current.EnvFile = version.EnvFile
	current.UpdatedAt = now()
	current.UpdatedBy = newOperator(user)
	if uErr := b.di.ComposeStackUpdate(ctx, current); uErr != nil {
		return uErr
	}
	b.eb.CreateStack(EventActionUpdate, current.HostID, current.Name, user)
	return nil
}
