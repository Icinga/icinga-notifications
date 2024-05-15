package contracts

import (
	"fmt"
	"github.com/icinga/icinga-notifications/internal/object"
	"go.uber.org/zap"
)

// NotifyContext is a notification context passed to the channel#Notify() method and
// provides all the necessary information for sending a notification. Currently, it's
// implemented by the Incident and DefaultNotifyCtx types, whilst the latter can be used
// to send notifications without a corresponding active Incident.
type NotifyContext interface {
	// GetObject returns the appropriate object for which the notification is to be sent.
	// It should only be used to access/read the object state, but not to modify it!
	GetObject() *object.Object

	// Logger retrieves the underlying logger associated to this specific notification context.
	Logger() *zap.SugaredLogger
}

type Incident interface {
	fmt.Stringer
	NotifyContext

	ID() int64
	SeverityString() string
}

// DefaultNotifyCtx implements the NotifyContext interface.
// You can use this type if you want to trigger notifications without an active incident.
// Use the NewDefaultNotifyCtx function to construct a fully initialised instance of this type.
type DefaultNotifyCtx struct {
	obj    *object.Object
	logger *zap.SugaredLogger
}

// NewDefaultNotifyCtx creates a fully initialised DefaultNotifyCtx instance.
func NewDefaultNotifyCtx(obj *object.Object, l *zap.SugaredLogger) *DefaultNotifyCtx {
	return &DefaultNotifyCtx{obj: obj, logger: l}
}

func (dnc *DefaultNotifyCtx) GetObject() *object.Object {
	return dnc.obj
}

func (dnc *DefaultNotifyCtx) Logger() *zap.SugaredLogger {
	return dnc.logger
}
