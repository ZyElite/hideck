package device

import "sort"

// WorkerLabel is an immutable device identity snapshot for callers that do not
// need access to the mutable Worker lifecycle.
type WorkerLabel struct {
	ID   string
	Name string
}

// WorkerLabels copies device IDs and names while the pool lock is held.
func (p *Pool) WorkerLabels() []WorkerLabel {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	labels := make([]WorkerLabel, 0, len(p.workers))
	for _, worker := range p.workers {
		if worker != nil {
			labels = append(labels, WorkerLabel{ID: worker.ID, Name: worker.Config.Name})
		}
	}
	p.mu.RUnlock()
	sort.Slice(labels, func(i, j int) bool { return labels[i].ID < labels[j].ID })
	return labels
}

// WorkerName returns a copied device name without exposing a mutable Worker.
func (p *Pool) WorkerName(deviceID string) string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	worker := p.workers[deviceID]
	if worker == nil {
		return ""
	}
	return worker.Config.Name
}
