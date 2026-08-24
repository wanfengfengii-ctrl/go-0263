package api

import (
	"net/http"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
	"github.com/olivepress/fruit-intake-gate/task"
)

func (s *Server) handleGetPlot(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPlot(r.Context(), catalog.PlotID(r.PathValue("id")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	rule, err := s.store.GetRule(r.Context(), catalog.RuleDigest(r.PathValue("digest")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	var req store.LockRequest
	if !decodeBody(w, r, &req) {
		return
	}
	view, err := s.store.LockTask(r.Context(), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := task.TaskID(r.PathValue("id"))
	view, err := s.store.GetTaskView(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSampleConfirm(w http.ResponseWriter, r *http.Request) {
	var req store.SampleConfirmRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.SampleConfirm(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleSplitSamples(w http.ResponseWriter, r *http.Request) {
	var req store.SplitSamplesRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.SplitSamples(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	var req store.RevealRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.RevealSamples(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleStartResources(w http.ResponseWriter, r *http.Request) {
	var req store.StartResourcesRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.StartResources(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleMaturityCounts(w http.ResponseWriter, r *http.Request) {
	var req store.MaturityCountsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.MaturityCounts(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleReadings(w http.ResponseWriter, r *http.Request) {
	var req store.ReadingsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.SubmitReadings(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleForeignMatter(w http.ResponseWriter, r *http.Request) {
	var req store.ForeignMatterRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.ForeignMatter(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleRejudge(w http.ResponseWriter, r *http.Request) {
	var req store.RejudgeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.Rejudge(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req store.ReviewRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.Review(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	var req store.FinalizeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.mutate(w, r, func() (*store.TaskView, error) {
		return s.store.Finalize(r.Context(), task.TaskID(r.PathValue("id")), req)
	})
}

// mutate runs a mutating operation and writes a uniform success or error
// response.
func (s *Server) mutate(w http.ResponseWriter, r *http.Request, fn func() (*store.TaskView, error)) {
	view, err := fn()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
