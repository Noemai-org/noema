package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// recordpools.go builds the Record reader's per-tenant pools (EDG-01 slice 3c, part 3; ruling A170 and the default for OD-P-82): a directory with one file per tenant, named
// by the tenant ID, each holding that tenant's record_read DSN. The database, not this code, decides which tenant's chain a connection can read (DEBT-04): this code only
// refuses to hand out any pool but the tenant's own. A DSN holds a password, so no message here carries a DSN or any part of one (AGENT-13).

// EnvRecordReadDSNDir names the directory of DSN files. Unset means no Record reader; there is no default directory.
const EnvRecordReadDSNDir = "NOEMA_RECORD_READ_DSN_DIR"

// tenantFileRe is the name of a DSN file: a tenant ID of common.v1, as written (anchored, case-sensitive).
var tenantFileRe = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)

// recordPools is a reconstruct.TenantDBs over one pool per tenant.
type recordPools struct{ pools map[string]*pgxpool.Pool }

// For returns the tenant's own pool, or an error. It never falls back to another tenant's pool or to a default.
func (p *recordPools) For(tenantID string) (reconstruct.TxBeginner, error) {
	pool, ok := p.pools[tenantID]
	if !ok || !tenantFileRe.MatchString(tenantID) {
		return nil, errors.New("cmd/core: no Record reader is configured for this tenant")
	}
	return pool, nil
}

// buildRecordReader reads the DSN files and returns the reader and a function that closes every pool (safe to call more than once). With the setting unset it returns no
// reader, a function that does nothing and no error. With it set, any file that cannot be applied safely is an error naming the setting or the tenant, with no reader and
// no pool left open. Pools connect lazily: nothing is dialled here.
func buildRecordReader(ctx context.Context, env Env) (reconstruct.TenantDBs, func(), error) {
	noop := func() {}
	dir := env.Getenv(EnvRecordReadDSNDir)
	if dir == "" {
		return nil, noop, nil
	}
	if env.ReadDir == nil || env.ReadFile == nil {
		return nil, nil, fmt.Errorf("cmd/core: %s cannot be read: the process has no file access wired", EnvRecordReadDSNDir)
	}
	entries, err := env.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: %s: the directory cannot be read", EnvRecordReadDSNDir)
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("cmd/core: %s: the directory holds no tenant file", EnvRecordReadDSNDir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !tenantFileRe.MatchString(e.Name()) {
			return nil, nil, fmt.Errorf("cmd/core: %s: an entry is not a file named by a tenant ID", EnvRecordReadDSNDir)
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	pools := make(map[string]*pgxpool.Pool, len(names))
	closeAll := func() {
		for _, p := range pools {
			p.Close()
		}
	}
	for _, tenant := range names {
		raw, err := env.ReadFile(filepath.Join(dir, tenant))
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the file of tenant %s cannot be read", EnvRecordReadDSNDir, tenant)
		}
		dsn := strings.TrimSpace(string(raw))
		if dsn == "" {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the file of tenant %s is empty", EnvRecordReadDSNDir, tenant)
		}
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil { // the parse error is dropped on purpose: it can quote the DSN
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the DSN of tenant %s does not parse", EnvRecordReadDSNDir, tenant)
		}
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			closeAll()
			return nil, nil, fmt.Errorf("cmd/core: %s: the pool of tenant %s cannot be made", EnvRecordReadDSNDir, tenant)
		}
		pools[tenant] = pool
	}
	var once sync.Once
	return &recordPools{pools: pools}, func() { once.Do(closeAll) }, nil
}
