package mongo

import (
	"context"

	"github.com/cuigh/swirl/dao"
	"go.mongodb.org/mongo-driver/bson"
)

const Host = "host"

func (d *Dao) HostCreate(ctx context.Context, host *dao.Host) (err error) {
	return d.create(ctx, Host, host)
}

func (d *Dao) HostUpdate(ctx context.Context, host *dao.Host) (err error) {
	update := bson.M{
		"$set": bson.M{
			"name":        host.Name,
			"endpoint":    host.Endpoint,
			"auth_method": host.AuthMethod,
			"tls_ca_cert": host.TLSCACert,
			"tls_cert":    host.TLSCert,
			"ssh_user":    host.SSHUser,
			"updated_at":  host.UpdatedAt,
			"updated_by":  host.UpdatedBy,
		},
	}
	if host.TLSKey != "" {
		update["$set"].(bson.M)["tls_key"] = host.TLSKey
	}
	if host.SSHKey != "" {
		update["$set"].(bson.M)["ssh_key"] = host.SSHKey
	}
	return d.update(ctx, Host, host.ID, update)
}

func (d *Dao) HostUpdateStatus(ctx context.Context, id, status, errMsg, engineVer, os, arch string, cpus int, memory int64) error {
	set := bson.M{
		"status": status,
		"error":  errMsg,
	}
	if engineVer != "" {
		set["engine_ver"] = engineVer
	}
	if os != "" {
		set["os"] = os
	}
	if arch != "" {
		set["arch"] = arch
	}
	if cpus > 0 {
		set["cpus"] = cpus
	}
	if memory > 0 {
		set["memory"] = memory
	}
	return d.update(ctx, Host, id, bson.M{"$set": set})
}

func (d *Dao) HostGet(ctx context.Context, id string) (host *dao.Host, err error) {
	host = &dao.Host{}
	found, err := d.find(ctx, Host, id, host)
	if !found {
		return nil, err
	}
	return
}

func (d *Dao) HostGetAll(ctx context.Context) (hosts []*dao.Host, err error) {
	hosts = []*dao.Host{}
	err = d.fetch(ctx, Host, bson.M{}, &hosts)
	return
}

func (d *Dao) HostSearch(ctx context.Context, args *dao.HostSearchArgs) (hosts []*dao.Host, count int, err error) {
	filter := bson.M{}
	if args.Name != "" {
		filter["name"] = args.Name
	}
	if args.Status != "" {
		filter["status"] = args.Status
	}
	opts := searchOptions{filter: filter, pageIndex: args.PageIndex, pageSize: args.PageSize}
	hosts = []*dao.Host{}
	count, err = d.search(ctx, Host, opts, &hosts)
	return
}

func (d *Dao) HostDelete(ctx context.Context, id string) (err error) {
	return d.delete(ctx, Host, id)
}
