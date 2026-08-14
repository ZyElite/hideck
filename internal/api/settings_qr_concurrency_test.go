package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func requestQRStatusConcurrently(path string, handler func(*gin.Context)) []*httptest.ResponseRecorder {
	recorders := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(len(recorders))
	for _, recorder := range recorders {
		recorder := recorder
		go func() {
			defer wait.Done()
			<-start
			requestContext, _ := gin.CreateTestContext(recorder)
			requestContext.Request = httptest.NewRequest(http.MethodGet, path, nil)
			handler(requestContext)
		}()
	}
	close(start)
	wait.Wait()
	return recorders
}

func successfulQRStatus(t *testing.T, recorders []*httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	var success *httptest.ResponseRecorder
	notFound := 0
	for _, recorder := range recorders {
		switch recorder.Code {
		case http.StatusOK:
			if success != nil || strings.Contains(recorder.Body.String(), `"status":""`) {
				t.Fatalf("unexpected successful QR responses: %s", recorder.Body.String())
			}
			success = recorder
		case http.StatusNotFound:
			notFound++
		default:
			t.Fatalf("QR status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
	}
	if success == nil || notFound != 1 {
		t.Fatalf("QR status results: success=%v not_found=%d", success != nil, notFound)
	}
	return success
}
