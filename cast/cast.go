package cast

import (
	"context"
	"fmt"
	"time"

	"github.com/renproject/aw/protocol"
	"github.com/sirupsen/logrus"
)

type Caster interface {
	Cast(ctx context.Context, to protocol.PeerID, body protocol.MessageBody) error
	AcceptCast(ctx context.Context, message protocol.Message) error
}

type caster struct {
	messages protocol.MessageSender
	events   protocol.EventSender
	logger   logrus.FieldLogger
}

func NewCaster(messages protocol.MessageSender, events protocol.EventSender, logger logrus.FieldLogger) Caster {
	return &caster{
		messages: messages,
		events:   events,
		logger:   logger,
	}
}

func (caster *caster) Cast(ctx context.Context, to protocol.PeerID, body protocol.MessageBody) error {
	messageWire := protocol.MessageOnTheWire{
		To:      to,
		Message: protocol.NewMessage(protocol.V1, protocol.Cast, body),
	}
	select {
	case <-ctx.Done():
		return newErrCasting(to, ctx.Err())
	case caster.messages <- messageWire:
		return nil
	}
}

func (caster *caster) AcceptCast(ctx context.Context, message protocol.Message) error {
	// TODO: Update to allow message forwarding.
	// Pre-condition checks
	if message.Version != protocol.V1 {
		return newErrCastVersionNotSupported(message.Version)
	}
	if message.Variant != protocol.Cast {
		return newErrCastVariantNotSupported(message.Variant)
	}

	event := protocol.EventMessageReceived{
		Time:    time.Now(),
		Message: message.Body,
	}
	select {
	case <-ctx.Done():
		return newErrAcceptingCast(ctx.Err())
	case caster.events <- event:
		return nil
	}
}

type ErrCasting struct {
	error
	PeerID protocol.PeerID
}

func newErrCasting(peerID protocol.PeerID, err error) error {
	return ErrCasting{
		error:  fmt.Errorf("error casting to %v: %v", peerID, err),
		PeerID: peerID,
	}
}

type ErrAcceptingCast struct {
	error
}

func newErrAcceptingCast(err error) error {
	return ErrAcceptingCast{
		error: fmt.Errorf("error accepting cast: %v", err),
	}
}

// ErrCastVersionNotSupported is returned when a cast message has an
// unsupported version.
type ErrCastVersionNotSupported struct {
	error
}

func newErrCastVersionNotSupported(version protocol.MessageVersion) error {
	return ErrCastVersionNotSupported{
		error: fmt.Errorf("cast version=%v not supported", version),
	}
}

// ErrCastVariantNotSupported is returned when a cast message has an
// unsupported variant.
type ErrCastVariantNotSupported struct {
	error
}

func newErrCastVariantNotSupported(variant protocol.MessageVariant) error {
	return ErrCastVariantNotSupported{
		error: fmt.Errorf("cast variant=%v not supported", variant),
	}
}
