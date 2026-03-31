package job

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/mhsanaei/3x-ui/v2/logger"
	"github.com/mhsanaei/3x-ui/v2/web/service"
	"github.com/mhsanaei/3x-ui/v2/web/websocket"
	"github.com/mhsanaei/3x-ui/v2/xray"

	"github.com/valyala/fasthttp"
)

type XrayTrafficJob struct {
	settingService  service.SettingService
	xrayService     service.XrayService
	inboundService  service.InboundService
	outboundService service.OutboundService

	mu              sync.Mutex
	pendingInbound  []*xray.Traffic
	pendingOutbound []*xray.Traffic
	pendingClient   []*xray.ClientTraffic
	pendingDaily    []*xray.Traffic
	dbFlushing      atomic.Bool
}

func NewXrayTrafficJob() *XrayTrafficJob {
	return new(XrayTrafficJob)
}

func (j *XrayTrafficJob) Run() {
	if !j.xrayService.IsXrayRunning() {
		return
	}
	traffics, clientTraffics, err := j.xrayService.GetXrayTraffic()
	if err != nil {
		return
	}

	j.bufferTraffic(traffics, clientTraffics)

	go j.flushToDB()

	if ExternalTrafficInformEnable, err := j.settingService.GetExternalTrafficInformEnable(); ExternalTrafficInformEnable {
		j.informTrafficToExternalAPI(traffics, clientTraffics)
	} else if err != nil {
		logger.Warning("get ExternalTrafficInformEnable failed:", err)
	}

	onlineClients := j.inboundService.GetOnlineClients()
	lastOnlineMap, err := j.inboundService.GetClientsLastOnline()
	if err != nil {
		logger.Warning("get clients last online failed:", err)
		lastOnlineMap = make(map[string]int64)
	}

	updatedInbounds, err := j.inboundService.GetAllInbounds()
	if err != nil {
		logger.Warning("get all inbounds for websocket failed:", err)
	}

	updatedOutbounds, err := j.outboundService.GetOutboundsTraffic()
	if err != nil {
		logger.Warning("get all outbounds for websocket failed:", err)
	}

	trafficUpdate := map[string]any{
		"traffics":       traffics,
		"clientTraffics": clientTraffics,
		"onlineClients":  onlineClients,
		"lastOnlineMap":  lastOnlineMap,
	}
	websocket.BroadcastTraffic(trafficUpdate)

	if updatedInbounds != nil {
		websocket.BroadcastInbounds(updatedInbounds)
	}
	if updatedOutbounds != nil {
		websocket.BroadcastOutbounds(updatedOutbounds)
	}
}

// bufferTraffic accumulates traffic deltas in the pending buffer.
// If a previous DB flush is still running, deltas are merged so nothing is lost.
func (j *XrayTrafficJob) bufferTraffic(traffics []*xray.Traffic, clientTraffics []*xray.ClientTraffic) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.pendingInbound = mergeTraffics(j.pendingInbound, traffics, true)
	j.pendingOutbound = mergeTraffics(j.pendingOutbound, traffics, false)
	j.pendingClient = mergeClientTraffics(j.pendingClient, clientTraffics)
	j.pendingDaily = mergeTraffics(j.pendingDaily, traffics, true)
}

// flushToDB writes all buffered traffic to MySQL/SQLite.
// Uses a CAS flag to prevent concurrent flushes.
func (j *XrayTrafficJob) flushToDB() {
	if !j.dbFlushing.CompareAndSwap(false, true) {
		return
	}
	defer j.dbFlushing.Store(false)

	j.mu.Lock()
	inbound := j.pendingInbound
	outbound := j.pendingOutbound
	client := j.pendingClient
	daily := j.pendingDaily
	j.pendingInbound = nil
	j.pendingOutbound = nil
	j.pendingClient = nil
	j.pendingDaily = nil
	j.mu.Unlock()

	if len(inbound) > 0 || len(client) > 0 {
		err, needRestart := j.inboundService.AddTraffic(inbound, client)
		if err != nil {
			logger.Warning("flush inbound traffic failed:", err)
			j.requeue(inbound, nil, client, nil)
		} else if needRestart {
			j.xrayService.SetToNeedRestart()
		}
	}

	if len(daily) > 0 {
		j.inboundService.AccumulateDailyTraffic(daily)
	}

	if len(outbound) > 0 {
		err, _ := j.outboundService.AddTraffic(outbound, nil)
		if err != nil {
			logger.Warning("flush outbound traffic failed:", err)
			j.requeue(nil, outbound, nil, nil)
		}
	}
}

// requeue puts failed traffic back into the pending buffer for the next flush.
func (j *XrayTrafficJob) requeue(inbound, outbound []*xray.Traffic, client []*xray.ClientTraffic, daily []*xray.Traffic) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if len(inbound) > 0 {
		j.pendingInbound = mergeTraffics(inbound, j.pendingInbound, true)
	}
	if len(outbound) > 0 {
		j.pendingOutbound = mergeTraffics(outbound, j.pendingOutbound, false)
	}
	if len(client) > 0 {
		j.pendingClient = mergeClientTraffics(client, j.pendingClient)
	}
	if len(daily) > 0 {
		j.pendingDaily = mergeTraffics(daily, j.pendingDaily, true)
	}
}

// mergeTraffics merges two traffic slices, summing up/down for matching tags.
func mergeTraffics(existing, incoming []*xray.Traffic, isInbound bool) []*xray.Traffic {
	if len(incoming) == 0 {
		return existing
	}

	m := make(map[string]*xray.Traffic, len(existing))
	for _, t := range existing {
		m[t.Tag] = t
	}
	for _, t := range incoming {
		if isInbound && !t.IsInbound {
			continue
		}
		if !isInbound && !t.IsOutbound {
			continue
		}
		if e, ok := m[t.Tag]; ok {
			e.Up += t.Up
			e.Down += t.Down
		} else {
			clone := *t
			m[t.Tag] = &clone
		}
	}
	result := make([]*xray.Traffic, 0, len(m))
	for _, t := range m {
		result = append(result, t)
	}
	return result
}

// mergeClientTraffics merges two client traffic slices, summing up/down for matching emails.
func mergeClientTraffics(existing, incoming []*xray.ClientTraffic) []*xray.ClientTraffic {
	if len(incoming) == 0 {
		return existing
	}
	if len(existing) == 0 {
		result := make([]*xray.ClientTraffic, len(incoming))
		copy(result, incoming)
		return result
	}

	m := make(map[string]*xray.ClientTraffic, len(existing))
	for _, t := range existing {
		m[t.Email] = t
	}
	for _, t := range incoming {
		if e, ok := m[t.Email]; ok {
			e.Up += t.Up
			e.Down += t.Down
		} else {
			clone := *t
			m[t.Email] = &clone
		}
	}
	result := make([]*xray.ClientTraffic, 0, len(m))
	for _, t := range m {
		result = append(result, t)
	}
	return result
}

func (j *XrayTrafficJob) informTrafficToExternalAPI(inboundTraffics []*xray.Traffic, clientTraffics []*xray.ClientTraffic) {
	informURL, err := j.settingService.GetExternalTrafficInformURI()
	if err != nil {
		logger.Warning("get ExternalTrafficInformURI failed:", err)
		return
	}
	requestBody, err := json.Marshal(map[string]any{"clientTraffics": clientTraffics, "inboundTraffics": inboundTraffics})
	if err != nil {
		logger.Warning("parse client/inbound traffic failed:", err)
		return
	}
	request := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(request)
	request.Header.SetMethod("POST")
	request.Header.SetContentType("application/json; charset=UTF-8")
	request.SetBody([]byte(requestBody))
	request.SetRequestURI(informURL)
	response := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(response)
	if err := fasthttp.Do(request, response); err != nil {
		logger.Warning("POST ExternalTrafficInformURI failed:", err)
	}
}
