package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"solana-meme-backtest/backend/internal/eventbus"
	"solana-meme-backtest/backend/internal/model"
	"solana-meme-backtest/backend/internal/response"
)

func (h *Handler) streamCandidateMonitor(c *gin.Context) {
	h.streamTopic(c, eventbus.TopicCandidates, func() (any, error) {
		if h.candidateMonitor == nil {
			return gin.H{"items": []any{}}, nil
		}
		items, err := h.candidateMonitor.ListCandidates(c.Request.Context())
		if err != nil {
			return nil, err
		}
		return gin.H{"items": items}, nil
	}, nil)
}

func (h *Handler) streamTradeSignals(c *gin.Context) {
	mode, ok := parseTradeModeValue(c.Query("tradeMode"))
	if !ok {
		responseBadTradeMode(c)
		return
	}
	limit := parseLimit(c.Query("limit"), 100)
	h.streamTopic(c, eventbus.TopicSignals, func() (any, error) {
		items, err := h.tradeService.ListSignals(c.Request.Context(), mode, limit)
		if err != nil {
			return nil, err
		}
		return gin.H{"items": items}, nil
	}, func(event eventbus.Event) (eventbus.Event, bool) {
		item, ok := event.Data.(model.TradeSignal)
		if !ok || !matchesTradeMode(item.TradeMode, mode) {
			return eventbus.Event{}, false
		}
		return event, true
	})
}

func (h *Handler) streamTradeOrders(c *gin.Context) {
	mode, ok := parseTradeModeValue(c.Query("tradeMode"))
	if !ok {
		responseBadTradeMode(c)
		return
	}
	limit := parseLimit(c.Query("limit"), 100)
	h.streamTopic(c, eventbus.TopicOrders, func() (any, error) {
		items, err := h.tradeService.ListOrders(c.Request.Context(), mode, limit)
		if err != nil {
			return nil, err
		}
		return gin.H{"items": items}, nil
	}, func(event eventbus.Event) (eventbus.Event, bool) {
		item, ok := event.Data.(model.TradeOrder)
		if !ok || !matchesTradeMode(item.TradeMode, mode) {
			return eventbus.Event{}, false
		}
		return event, true
	})
}

func (h *Handler) streamTradePositions(c *gin.Context) {
	mode, ok := parseTradeModeValue(c.Query("tradeMode"))
	if !ok {
		responseBadTradeMode(c)
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	limit := parseLimit(c.Query("limit"), 100)
	h.streamTopic(c, eventbus.TopicPositions, func() (any, error) {
		items, err := h.tradeService.ListPositions(c.Request.Context(), status, mode, limit)
		if err != nil {
			return nil, err
		}
		return gin.H{"items": items}, nil
	}, func(event eventbus.Event) (eventbus.Event, bool) {
		item, ok := event.Data.(model.TradePosition)
		if !ok || !matchesTradeMode(item.TradeMode, mode) {
			return eventbus.Event{}, false
		}
		if status != "" && string(item.Status) != status {
			return eventbus.Event{Type: eventbus.EventDelete, ID: item.ID, Data: item.ID}, true
		}
		return event, true
	})
}

func (h *Handler) streamTopic(c *gin.Context, topic string, snapshot func() (any, error), filter func(eventbus.Event) (eventbus.Event, bool)) {
	if h.eventBus == nil {
		response.Fail(c, http.StatusInternalServerError, "实时事件总线未初始化")
		return
	}
	ch, cancel := h.eventBus.Subscribe(topic)
	defer cancel()

	snapshotData, err := snapshot()
	if err != nil {
		h.handleError(c, err)
		return
	}

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := writeSSE(w, eventbus.EventSnapshot, snapshotData); err != nil {
		return
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if err := writeSSE(w, eventbus.EventHeartbeat, gin.H{"ts": time.Now().UTC().Format(time.RFC3339)}); err != nil {
				return
			}
		case event, ok := <-ch:
			if !ok {
				return
			}
			if filter != nil {
				var keep bool
				event, keep = filter(event)
				if !keep {
					continue
				}
			}
			if err := writeStreamEvent(w, event); err != nil {
				return
			}
		}
	}
}

func writeStreamEvent(w gin.ResponseWriter, event eventbus.Event) error {
	switch event.Type {
	case eventbus.EventUpsert:
		return writeSSE(w, eventbus.EventUpsert, gin.H{"item": event.Data})
	case eventbus.EventDelete:
		return writeSSE(w, eventbus.EventDelete, gin.H{"id": event.ID})
	default:
		return nil
	}
}

func writeSSE(w gin.ResponseWriter, event string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
		return err
	}
	w.Flush()
	return nil
}

func parseTradeModeValue(value string) (model.TradeMode, bool) {
	switch value {
	case "", "all":
		return "", true
	case string(model.TradeModePaper):
		return model.TradeModePaper, true
	case string(model.TradeModeLive):
		return model.TradeModeLive, true
	default:
		return "", false
	}
}

func responseBadTradeMode(c *gin.Context) {
	response.Fail(c, http.StatusBadRequest, "tradeMode 仅支持 all/paper/live")
}

func matchesTradeMode(itemMode model.TradeMode, filter model.TradeMode) bool {
	return filter == "" || itemMode == filter
}
