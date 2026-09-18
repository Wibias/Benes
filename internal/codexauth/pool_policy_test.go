package codexauth

import (
	"fmt"
	"testing"
)

func TestProjectPoolRoutingPolicyPreservesUnsetDefaults(t *testing.T) {
	policy, err := ProjectPoolRoutingPolicy([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if policy.Strategy != "" || policy.StickyLimit != 0 || policy.AutoSwitchThreshold != nil || policy.FailoverThreshold != nil {
		t.Fatalf("policy=%#v", policy)
	}
}

func TestProjectPoolRoutingPolicyProjectsExplicitValues(t *testing.T) {
	policy, err := ProjectPoolRoutingPolicy([]byte("{\"accountPoolStrategy\":\"reset-window\",\"accountPoolResetOrder\":\"latest\",\"accountPoolStickyLimit\":7,\"autoSwitchThreshold\":63,\"upstreamFailoverThreshold\":5}"))
	if err != nil {
		t.Fatal(err)
	}
	if policy.Strategy != PoolStrategyResetWindow || policy.ResetOrder != ResetOrderLatest || policy.StickyLimit != 7 {
		t.Fatalf("policy=%#v", policy)
	}
	if policy.AutoSwitchThreshold == nil || *policy.AutoSwitchThreshold != 63 {
		t.Fatalf("auto switch=%v", policy.AutoSwitchThreshold)
	}
	if policy.FailoverThreshold == nil || *policy.FailoverThreshold != 5 {
		t.Fatalf("failover=%v", policy.FailoverThreshold)
	}
}

func TestProjectPoolRoutingPolicyPreservesExplicitDisabledThresholds(t *testing.T) {
	policy, err := ProjectPoolRoutingPolicy([]byte("{\"autoSwitchThreshold\":0,\"upstreamFailoverThreshold\":0}"))
	if err != nil {
		t.Fatal(err)
	}
	if policy.AutoSwitchThreshold == nil || *policy.AutoSwitchThreshold != 0 {
		t.Fatalf("auto switch=%v", policy.AutoSwitchThreshold)
	}
	if policy.FailoverThreshold == nil || *policy.FailoverThreshold != 0 {
		t.Fatalf("failover=%v", policy.FailoverThreshold)
	}
}

func TestProjectPoolRoutingPolicyAcceptsAllStrategiesAndBoundaries(t *testing.T) {
	for _, strategy := range []PoolStrategy{PoolStrategyQuota, PoolStrategyRoundRobin, PoolStrategyFillFirst, PoolStrategyResetWindow} {
		raw := []byte(fmt.Sprintf("{\"accountPoolStrategy\":%q,\"accountPoolStickyLimit\":100,\"autoSwitchThreshold\":100,\"upstreamFailoverThreshold\":20}", strategy))
		policy, err := ProjectPoolRoutingPolicy(raw)
		if err != nil {
			t.Fatalf("strategy=%q err=%v raw=%s", strategy, err, raw)
		}
		if policy.Strategy != strategy || policy.StickyLimit != 100 || policy.AutoSwitchThreshold == nil || *policy.AutoSwitchThreshold != 100 || policy.FailoverThreshold == nil || *policy.FailoverThreshold != 20 {
			t.Fatalf("strategy=%q policy=%#v", strategy, policy)
		}
	}
}

func TestProjectPoolRoutingPolicyRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "root not object", raw: "[]"},
		{name: "strategy null", raw: "{\"accountPoolStrategy\":null}"},
		{name: "strategy type", raw: "{\"accountPoolStrategy\":1}"},
		{name: "strategy unknown", raw: "{\"accountPoolStrategy\":\"random\"}"},
		{name: "reset order unknown", raw: "{\"accountPoolResetOrder\":\"random\"}"},
		{name: "sticky null", raw: "{\"accountPoolStickyLimit\":null}"},
		{name: "sticky type", raw: "{\"accountPoolStickyLimit\":\"2\"}"},
		{name: "sticky fraction", raw: "{\"accountPoolStickyLimit\":1.5}"},
		{name: "sticky low", raw: "{\"accountPoolStickyLimit\":0}"},
		{name: "sticky high", raw: "{\"accountPoolStickyLimit\":101}"},
		{name: "auto null", raw: "{\"autoSwitchThreshold\":null}"},
		{name: "auto type", raw: "{\"autoSwitchThreshold\":\"80\"}"},
		{name: "auto fraction", raw: "{\"autoSwitchThreshold\":80.5}"},
		{name: "auto low", raw: "{\"autoSwitchThreshold\":-1}"},
		{name: "auto high", raw: "{\"autoSwitchThreshold\":101}"},
		{name: "failover null", raw: "{\"upstreamFailoverThreshold\":null}"},
		{name: "failover type", raw: "{\"upstreamFailoverThreshold\":\"3\"}"},
		{name: "failover fraction", raw: "{\"upstreamFailoverThreshold\":3.5}"},
		{name: "failover low", raw: "{\"upstreamFailoverThreshold\":-1}"},
		{name: "failover high", raw: "{\"upstreamFailoverThreshold\":21}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if policy, err := ProjectPoolRoutingPolicy([]byte(tc.raw)); err == nil {
				t.Fatalf("policy=%#v raw=%s", policy, tc.raw)
			}
		})
	}
}
