package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"solana-meme-backtest/backend/internal/response"
	"solana-meme-backtest/backend/internal/runtimeconfig"
	"solana-meme-backtest/backend/internal/trade"
)

func TestTradeRuntimeHandlersReturnAndPartiallyUpdateSwitches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	control, err := runtimeconfig.New(context.Background(), nil, runtimeconfig.State{
		CAMonitoringEnabled:   true,
		TradeExecutionEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{tradeService: &trade.Service{}, runtimeControl: control}
	router := gin.New()
	router.GET("/runtime", handler.getTradeRuntime)
	router.PUT("/runtime", handler.updateTradeRuntime)

	update := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/runtime", strings.NewReader(`{"caMonitoringEnabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(update, req)
	if update.Code != http.StatusOK {
		t.Fatalf("unexpected update status %d: %s", update.Code, update.Body.String())
	}
	assertRuntimeSwitches(t, update.Body.Bytes(), false, true)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/runtime", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("unexpected get status %d: %s", get.Code, get.Body.String())
	}
	assertRuntimeSwitches(t, get.Body.Bytes(), false, true)
}

func TestUpdateTradeRuntimeRejectsEmptyPayload(t *testing.T) {
	control, err := runtimeconfig.New(context.Background(), nil, runtimeconfig.State{})
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{tradeService: &trade.Service{}, runtimeControl: control}
	router := gin.New()
	router.PUT("/runtime", handler.updateTradeRuntime)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/runtime", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func assertRuntimeSwitches(t *testing.T, body []byte, caMonitoring bool, tradeExecution bool) {
	t.Helper()
	var payload response.Body
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	data, ok := payload.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected response data: %#v", payload.Data)
	}
	if data["caMonitoringEnabled"] != caMonitoring || data["tradeExecutionEnabled"] != tradeExecution {
		t.Fatalf("unexpected runtime data: %#v", data)
	}
}
