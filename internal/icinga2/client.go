package icinga2

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/icinga/icinga-notifications/internal/event"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"net/http"
	"net/url"
	"time"
)

// This file contains the main resp. common methods for the Client.

// eventMsg is an internal struct for passing events with additional information from producers to the dispatcher.
type eventMsg struct {
	event   *event.Event
	apiTime time.Time
}

// catchupEventMsg propagates either an eventMsg or an error back from the catch-up worker.
//
// The type must be used as a sum-type like data structure holding either an error or an eventMsg pointer. The error has
// a higher precedence than the eventMsg.
type catchupEventMsg struct {
	*eventMsg
	error
}

// Client for the Icinga 2 Event Stream API with support for other Icinga 2 APIs to gather additional information and
// perform a catch-up of unknown events either when starting up to or in case of a connection loss.
//
// Within the icinga-notifications scope, one or multiple Client instances can be generated from the configuration by
// calling NewClientsFromConfig.
//
// A Client must be started by calling its Process method, which blocks until Ctx is marked as done. Reconnections and
// the necessary state replaying in an internal catch-up-phase from the Icinga 2 API will be taken care off. Internally,
// the Client executes a worker within its own goroutine, which dispatches event.Event to the CallbackFn and enforces
// order during catching up after (re-)connections.
type Client struct {
	// ApiBaseURL et al. configure where and how the Icinga 2 API can be reached.
	ApiBaseURL       string
	ApiBasicAuthUser string
	ApiBasicAuthPass string
	ApiHttpTransport http.RoundTripper

	// EventSourceId to be reflected in generated event.Events.
	EventSourceId int64
	// IcingaWebRoot points to the Icinga Web 2 endpoint for generated URLs.
	IcingaWebRoot string

	// CallbackFn receives generated event.Event objects.
	CallbackFn func(*event.Event)
	// Ctx for all web requests as well as internal wait loops. The CtxCancel can be used to stop this Client.
	// Both fields are being populated with a new context from the NewClientFromConfig function.
	Ctx       context.Context
	CtxCancel context.CancelFunc
	// Logger to log to.
	Logger *zap.SugaredLogger

	// eventDispatcherEventStream communicates Events to be processed from the Event Stream API.
	eventDispatcherEventStream chan *eventMsg
	// catchupPhaseRequest requests the main worker to switch to the catch-up-phase to query the API for missed events.
	catchupPhaseRequest chan struct{}
}

// buildCommonEvent creates an event.Event based on Host and (optional) Service attributes to be specified later.
//
// The new Event's Time will be the current timestamp.
//
// The following fields will NOT be populated and might be altered later:
//   - Type
//   - Severity
//   - Username
//   - Message
//   - ID
func (client *Client) buildCommonEvent(ctx context.Context, host, service string) (*event.Event, error) {
	var (
		eventName      string
		eventUrl       *url.URL
		eventTags      map[string]string
		eventExtraTags = make(map[string]string)
	)

	eventUrl, err := url.Parse(client.IcingaWebRoot)
	if err != nil {
		return nil, err
	}

	if service != "" {
		eventName = host + "!" + service

		eventUrl = eventUrl.JoinPath("/icingadb/service")
		eventUrl.RawQuery = "name=" + rawurlencode(service) + "&host.name=" + rawurlencode(host)

		eventTags = map[string]string{
			"host":    host,
			"service": service,
		}

		serviceGroups, err := client.fetchHostServiceGroups(ctx, host, service)
		if err != nil {
			return nil, err
		}
		for _, serviceGroup := range serviceGroups {
			eventExtraTags["servicegroup/"+serviceGroup] = ""
		}
	} else {
		eventName = host

		eventUrl = eventUrl.JoinPath("/icingadb/host")
		eventUrl.RawQuery = "name=" + rawurlencode(host)

		eventTags = map[string]string{
			"host": host,
		}
	}

	hostGroups, err := client.fetchHostServiceGroups(ctx, host, "")
	if err != nil {
		return nil, err
	}
	for _, hostGroup := range hostGroups {
		eventExtraTags["hostgroup/"+hostGroup] = ""
	}

	return &event.Event{
		Time:      time.Now(),
		SourceId:  client.EventSourceId,
		Name:      eventName,
		URL:       eventUrl.String(),
		Tags:      eventTags,
		ExtraTags: eventExtraTags,
	}, nil
}

// buildHostServiceEvent constructs an event.Event based on a CheckResult, a Host or Service state, a Host name and an
// optional Service name if the Event should represent a Service object.
func (client *Client) buildHostServiceEvent(ctx context.Context, result CheckResult, state int, host, service string) (*event.Event, error) {
	var eventSeverity event.Severity

	if service != "" {
		switch state {
		case StateServiceOk:
			eventSeverity = event.SeverityOK
		case StateServiceWarning:
			eventSeverity = event.SeverityWarning
		case StateServiceCritical:
			eventSeverity = event.SeverityCrit
		default: // UNKNOWN or faulty
			eventSeverity = event.SeverityErr
		}
	} else {
		switch state {
		case StateHostUp:
			eventSeverity = event.SeverityOK
		case StateHostDown:
			eventSeverity = event.SeverityCrit
		default: // faulty
			eventSeverity = event.SeverityErr
		}
	}

	ev, err := client.buildCommonEvent(ctx, host, service)
	if err != nil {
		return nil, err
	}

	ev.Type = event.TypeState
	ev.Severity = eventSeverity
	ev.Message = result.Output

	return ev, nil
}

// buildAcknowledgementEvent from the given fields.
func (client *Client) buildAcknowledgementEvent(
	ctx context.Context, host, service, author, comment string, clearEvent bool,
) (*event.Event, error) {
	ev, err := client.buildCommonEvent(ctx, host, service)
	if err != nil {
		return nil, err
	}

	if clearEvent {
		ev.Type = event.TypeAcknowledgementCleared
	} else {
		ev.Type = event.TypeAcknowledgementSet
	}

	ev.Username = author
	ev.Message = comment

	return ev, nil
}

// buildDowntimeEvent from the given fields.
func (client *Client) buildDowntimeEvent(ctx context.Context, d Downtime, startEvent bool) (*event.Event, error) {
	ev, err := client.buildCommonEvent(ctx, d.Host, d.Service)
	if err != nil {
		return nil, err
	}

	if startEvent {
		ev.Type = event.TypeDowntimeStart
	} else if d.RemoveTime.Time().IsZero() {
		ev.Type = event.TypeDowntimeEnd
	} else {
		ev.Type = event.TypeDowntimeRemoved
	}

	ev.Username = d.Author
	ev.Message = d.Comment

	return ev, nil
}

// buildFlappingEvent from the given fields.
func (client *Client) buildFlappingEvent(ctx context.Context, host, service string, state, stateType int, isFlapping bool) (*event.Event, error) {
	ev, err := client.buildCommonEvent(ctx, host, service)
	if err != nil {
		return nil, err
	}

	ev.Type = event.TypeFlappingStart
	if !isFlapping {
		ev.Type = event.TypeFlappingEnd
	}

	return ev, nil
}

// startCatchupWorkers launches goroutines for catching up the Icinga 2 API state.
//
// Each event will be sent to the returned channel. When all launched workers have finished - either because all are
// done or one has failed and the others were interrupted -, the channel will be closed. In case of a failure, _one_
// final error will be sent back.
//
// Those workers honor a context derived from the Client.Ctx and would either stop when this context is done or when the
// context.CancelFunc is called.
//
// The startup time might be delayed through the parameter. This lets the goroutines sleep to rate-limit reconnection
// attempts during network hiccups.
//
// To distinguish different catch-up-phase workers - for example, when one worker was canceled by its context and
// another one was just started -, all log their debug messages with a UUID.
func (client *Client) startCatchupWorkers(delay time.Duration) (chan *catchupEventMsg, context.CancelFunc) {
	workerId := uuid.New()
	startTime := time.Now()
	catchupEventCh := make(chan *catchupEventMsg)

	client.Logger.Debugw("Catch-up-phase worker has started",
		zap.Stringer("worker", workerId),
		zap.Duration("delay", delay))

	// Unfortunately, the errgroup context is hidden, that's why another context is necessary.
	ctx, cancel := context.WithCancel(client.Ctx)
	group, groupCtx := errgroup.WithContext(ctx)

	objTypes := []string{"host", "service"}
	for _, objType := range objTypes {
		objType := objType // https://go.dev/doc/faq#closures_and_goroutines
		group.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}

			err := client.checkMissedChanges(groupCtx, objType, catchupEventCh)
			if err != nil && !errors.Is(err, context.Canceled) {
				client.Logger.Debugw("Catch-up-phase event worker failed",
					zap.Stringer("worker", workerId),
					zap.String("object type", objType),
					zap.Error(err))
			}
			return err
		})
	}

	go func() {
		err := group.Wait()
		if err == nil {
			client.Logger.Debugw("Catching up the API has finished",
				zap.Stringer("worker", workerId),
				zap.Duration("duration", time.Since(startTime)))
		} else if errors.Is(err, context.Canceled) {
			// The context is either canceled when the Client got canceled or, more likely, when another catch-up-worker
			// was requested. In the first case, the already sent messages will be discarded as the worker's main loop
			// was left. In the other case, the message buffers will be reset to an empty state.
			client.Logger.Debugw("Catching up the API was interrupted",
				zap.Stringer("worker", workerId),
				zap.Duration("duration", time.Since(startTime)))
		} else {
			client.Logger.Debugw("Catching up the API failed",
				zap.Stringer("worker", workerId),
				zap.Error(err),
				zap.Duration("duration", time.Since(startTime)))

			select {
			case <-ctx.Done():
			case catchupEventCh <- &catchupEventMsg{error: err}:
			}
		}

		cancel()
		close(catchupEventCh)
	}()

	return catchupEventCh, cancel
}

// worker is the Client's main background worker, taking care of event.Event dispatching and mode switching.
//
// When the Client is in the catch-up-phase, requested by catchupPhaseRequest, events from the Event Stream API will
// be cached until the catch-up-phase has finished, while replayed events will be delivered directly.
//
// Communication takes place over the eventDispatcherEventStream and catchupPhaseRequest channels.
func (client *Client) worker() {
	var (
		// catchupEventCh either emits events generated during the catch-up-phase from catch-up-workers or one final
		// error if something went wrong. It will be closed when catching up is done, which indicates the select below
		// to switch phases. When this variable is nil, this Client is in the normal operating phase.
		catchupEventCh chan *catchupEventMsg
		// catchupCancel cancels, if not nil, all running catch-up-workers, e.g., when restarting catching-up.
		catchupCancel context.CancelFunc

		// catchupBuffer holds Event Stream events to be replayed after the catch-up-phase has finished.
		catchupBuffer = make([]*event.Event, 0)
		// catchupCache maps event.Events.Name to API time to skip replaying outdated events.
		catchupCache = make(map[string]time.Time)

		// catchupWorkerDelay slows down future catch-up-phase workers if prior attempts have failed.
		catchupWorkerDelay time.Duration
	)

	// catchupReset resets all catchup variables to their initial empty state.
	catchupReset := func() {
		catchupEventCh, catchupCancel = nil, nil
		catchupBuffer = make([]*event.Event, 0)
		catchupCache = make(map[string]time.Time)
	}

	// catchupCacheUpdate updates the catchupCache if this eventMsg seems to be the latest of its kind.
	catchupCacheUpdate := func(ev *eventMsg) {
		ts, ok := catchupCache[ev.event.Name]
		if !ok || ev.apiTime.After(ts) {
			catchupCache[ev.event.Name] = ev.apiTime
		}
	}

	// catchupWorkerStart starts a catch-up-phase worker and stops already running workers, if necessary.
	catchupWorkerStart := func() {
		if catchupEventCh != nil {
			client.Logger.Debug("Switching to catch-up-phase was requested while still catching up, stopping old worker")
			catchupCancel()
		}

		client.Logger.Info("Worker enters catch-up-phase, start caching up on Event Stream events")
		catchupReset()
		catchupEventCh, catchupCancel = client.startCatchupWorkers(catchupWorkerDelay)
	}

	for {
		select {
		case <-client.Ctx.Done():
			client.Logger.Warnw("Closing down main worker as context is finished", zap.Error(client.Ctx.Err()))
			return

		case <-client.catchupPhaseRequest:
			catchupWorkerStart()

		case catchupMsg, ok := <-catchupEventCh:
			// Process an incoming event
			if ok && catchupMsg.error == nil {
				client.CallbackFn(catchupMsg.eventMsg.event)
				catchupCacheUpdate(catchupMsg.eventMsg)
				break
			}

			// Abort and restart the catch-up-phase when receiving an error.
			if ok && catchupMsg.error != nil {
				if catchupWorkerDelay == 0 {
					catchupWorkerDelay = time.Second
				} else {
					catchupWorkerDelay = min(3*time.Minute, 2*catchupWorkerDelay)
				}

				client.Logger.Warnw("Catch-up-phase was interrupted by an error, another attempt will be made",
					zap.Error(catchupMsg.error),
					zap.Duration("delay", catchupWorkerDelay))

				catchupWorkerStart()
				break
			}

			// The channel is closed, replay cache and eventually switch modes
			if len(catchupBuffer) > 0 {
				// To not block the select and all channels too long, only one event will be processed per iteration.
				ev := catchupBuffer[0]
				catchupBuffer = catchupBuffer[1:]

				ts, ok := catchupCache[ev.Name]
				if !ok {
					client.Logger.Debugw("Event to be replayed is not in cache", zap.Stringer("event", ev))
				} else if ev.Time.Before(ts) {
					client.Logger.Debugw("Skip replaying outdated Event Stream event", zap.Stringer("event", ev),
						zap.Time("event timestamp", ev.Time), zap.Time("cache timestamp", ts))
					break
				}

				client.CallbackFn(ev)
				break
			}

			client.Logger.Info("Worker leaves catch-up-phase, returning to normal operation")
			catchupReset()
			catchupWorkerDelay = 0

		case ev := <-client.eventDispatcherEventStream:
			// During catch-up-phase, buffer Event Stream events
			if catchupEventCh != nil {
				catchupBuffer = append(catchupBuffer, ev.event)
				catchupCacheUpdate(ev)
				break
			}

			client.CallbackFn(ev.event)
		}
	}
}

// Process incoming events and reconnect to the Event Stream with catching up on missed objects if necessary.
//
// This method blocks as long as the Client runs, which, unless Ctx is cancelled, is forever. While its internal loop
// takes care of reconnections, messages are being logged while generated event.Event will be dispatched to the
// CallbackFn function.
func (client *Client) Process() {
	client.eventDispatcherEventStream = make(chan *eventMsg)
	client.catchupPhaseRequest = make(chan struct{})

	go client.worker()

	for client.Ctx.Err() == nil {
		err := client.listenEventStream()
		if err != nil {
			client.Logger.Errorw("Event Stream processing was interrupted", zap.Error(err))
		} else {
			client.Logger.Errorw("Event Stream processing was closed")
		}
	}
}
