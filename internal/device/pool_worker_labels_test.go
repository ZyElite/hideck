package device

import (
	"fmt"
	"sync"
	"testing"

	"github.com/yibaiba/hideck/internal/config"
)

func TestPoolWorkerLabelsReturnsSortedValueSnapshots(t *testing.T) {
	pool := &Pool{workers: map[string]*Worker{
		"wwan2": {ID: "wwan2", Config: config.DeviceConfig{Name: "备用卡"}},
		"wwan0": {ID: "wwan0", Config: config.DeviceConfig{Name: "主卡"}},
	}}

	labels := pool.WorkerLabels()
	if len(labels) != 2 || labels[0].ID != "wwan0" || labels[0].Name != "主卡" || labels[1].ID != "wwan2" {
		t.Fatalf("WorkerLabels() = %+v", labels)
	}
	labels[0].Name = "changed"
	if got := pool.WorkerName("wwan0"); got != "主卡" {
		t.Fatalf("WorkerName() = %q, want 主卡", got)
	}
}

func TestPoolWorkerLabelsConcurrentWithNameUpdates(t *testing.T) {
	pool := &Pool{workers: map[string]*Worker{
		"wwan0": {ID: "wwan0", Config: config.DeviceConfig{Name: "主卡"}},
	}}
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		for i := 0; i < 200; i++ {
			pool.UpdateWorkerConfig("wwan0", config.DeviceConfig{Name: fmt.Sprintf("card-%d", i)}, false)
		}
	}()
	go func() {
		defer wait.Done()
		for i := 0; i < 200; i++ {
			labels := pool.WorkerLabels()
			if len(labels) != 1 || labels[0].ID != "wwan0" {
				t.Errorf("WorkerLabels() = %+v", labels)
				return
			}
			_ = pool.WorkerName("wwan0")
		}
	}()
	wait.Wait()
}
