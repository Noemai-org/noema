package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// gatewaySettingNames is every setting of the Input Gateway and of its investigation workers, in the one order its gaps are named.
var gatewaySettingNames = []string{EnvEdgeDSNDir, EnvEdgeKeyDir, EnvEdgeKeyMaxActive, EnvInputHashKeyFile, EnvInputWindow, EnvInputRecordTimeout, edge.EnvMaxBody,
	EnvInvestigationWorkers, EnvInvestigationQueue, EnvInvestigationRunTimeoutMS, EnvInvestigationStartTimeoutMS}

// checkGatewaySetting refuses a given value that is invalid. The error names the setting and never its value. The three that name a directory or a file are judged when the gateway is
// built, because only building reads them.
func checkGatewaySetting(env Env, name string) error {
	switch name {
	case EnvEdgeKeyMaxActive:
		if n, err := strconv.Atoi(env.Getenv(name)); err != nil || n < 1 {
			return fmt.Errorf("cmd/core: %s must be a positive integer", name)
		}
	case edge.EnvMaxBody:
		if n, err := strconv.ParseInt(env.Getenv(name), 10, 64); err != nil || n < 1 {
			return fmt.Errorf("cmd/core: %s must be a positive integer", name)
		}
	case EnvInputWindow, EnvInputRecordTimeout:
		_, err := positiveDuration(env, name)
		return err
	case EnvInvestigationWorkers, EnvInvestigationQueue:
		_, err := wholePositive(env, name, maxPoolSetting)
		return err
	case EnvInvestigationRunTimeoutMS, EnvInvestigationStartTimeoutMS:
		_, err := wholePositive(env, name, maxReadTimeoutMS)
		return err
	}
	return nil
}

// buildGatewaySurface builds the Input Gateway and its investigation workers over a runner (STM-01 surfaces; REQ-INP-002, REQ-INP-003, REQ-API-001; INV-12, AGENT-16; A330, A469, A486,
// interim, relayed, not yet committed). A setting that is given and invalid is an error that names it. With none given the gateway is the plain gap of Components.Gateway. With some
// absent, or with no runner (the write path is not built) or no Recorder, the process starts and each reason is a gap line in fixed text, never a value; no worker is started and no
// pool opened. With every setting and a runner, the gateway is built and its closer closes the workers and the pools.
func buildGatewaySurface(ctx context.Context, env Env, clk clock.Clock, ids chain.IDSource, records reconstruct.TenantDBs, rec *orchestratorRecorder, runner turnRunner,
	failed func(string, error)) (*gatewayService, []string, error) {
	var absent []string
	given := 0
	for _, name := range gatewaySettingNames {
		if env.Getenv(name) == "" {
			absent = append(absent, name)
			continue
		}
		if err := checkGatewaySetting(env, name); err != nil {
			return nil, nil, err
		}
		if name != edge.EnvMaxBody { // the body bound alone is the edge's setting: it does not configure a gateway
			given++
		}
	}
	switch {
	case given == 0:
		return nil, nil, nil
	case len(absent) > 0:
		return nil, gapLines("input gateway", absent), nil
	case rec == nil || records == nil:
		return nil, []string{"input gateway: recorder not built"}, nil
	case runner == nil:
		return nil, []string{"input gateway: write path not built"}, nil
	}
	svc, err := buildGatewayService(ctx, env, clk, ids, records, recorderStarts{rec: rec}, runner, failed)
	if err != nil {
		return nil, nil, err
	}
	return svc, nil, nil
}
