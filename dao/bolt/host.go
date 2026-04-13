package bolt

import (
	"context"
	"strings"

	"github.com/cuigh/swirl/dao"
)

const Host = "host"

func (d *Dao) HostCreate(ctx context.Context, host *dao.Host) (err error) {
	return d.replace(Host, host.ID, host)
}

func (d *Dao) HostUpdate(ctx context.Context, host *dao.Host) (err error) {
	old := &dao.Host{}
	return d.update(Host, host.ID, old, func() interface{} {
		host.CreatedAt = old.CreatedAt
		host.CreatedBy = old.CreatedBy
		host.Status = old.Status
		host.EngineVer = old.EngineVer
		host.OS = old.OS
		host.Arch = old.Arch
		host.CPUs = old.CPUs
		host.Memory = old.Memory
		if host.TLSKey == "" {
			host.TLSKey = old.TLSKey
		}
		if host.SSHKey == "" {
			host.SSHKey = old.SSHKey
		}
		return host
	})
}

func (d *Dao) HostUpdateStatus(ctx context.Context, id, status, errMsg, engineVer string) error {
	old := &dao.Host{}
	return d.update(Host, id, old, func() interface{} {
		old.Status = status
		old.Error = errMsg
		old.EngineVer = engineVer
		return old
	})
}

func (d *Dao) HostGet(ctx context.Context, id string) (host *dao.Host, err error) {
	host = &dao.Host{}
	err = d.get(Host, id, host)
	if err == ErrNoRecords {
		return nil, nil
	} else if err != nil {
		host = nil
	}
	return
}

func (d *Dao) HostGetAll(ctx context.Context) (hosts []*dao.Host, err error) {
	err = d.each(Host, func(v []byte) error {
		h := &dao.Host{}
		err = decode(v, h)
		if err != nil {
			return err
		}
		hosts = append(hosts, h)
		return nil
	})
	return
}

func (d *Dao) HostSearch(ctx context.Context, args *dao.HostSearchArgs) (hosts []*dao.Host, count int, err error) {
	err = d.each(Host, func(v []byte) error {
		h := &dao.Host{}
		if err = decode(v, h); err != nil {
			return err
		}
		if args.Name != "" && !strings.Contains(h.Name, args.Name) {
			return nil
		}
		if args.Status != "" && h.Status != args.Status {
			return nil
		}
		hosts = append(hosts, h)
		return nil
	})
	if err == nil {
		count = len(hosts)
	}
	return
}

func (d *Dao) HostDelete(ctx context.Context, id string) (err error) {
	return d.delete(Host, id)
}
