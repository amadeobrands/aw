package pingpong

import (
	"context"
	"fmt"
	"time"

	"github.com/renproject/aw/dht"
	"github.com/renproject/aw/protocol"
)

type PingPonger interface {
	Ping(ctx context.Context, to protocol.PeerID) error
	AcceptPing(ctx context.Context, message protocol.Message) error
	AcceptPong(ctx context.Context, message protocol.Message) error
}

type pingPonger struct {
	dht      dht.DHT
	messages protocol.MessageSender
	events   protocol.EventSender
	codec    protocol.PeerAddressCodec
	me       protocol.PeerAddress
}

func NewPingPonger(dht dht.DHT, messages protocol.MessageSender, events protocol.EventSender, codec protocol.PeerAddressCodec, me protocol.PeerAddress) PingPonger {
	return &pingPonger{
		dht:      dht,
		messages: messages,
		events:   events,
		codec:    codec,
		me:       me,
	}
}

func (pp *pingPonger) Ping(ctx context.Context, to protocol.PeerID) error {
	// TODO: Wrap errors in custom error types.

	peerAddr, err := pp.dht.PeerAddress(to)
	if err != nil {
		return err
	}
	if peerAddr == nil {
		return fmt.Errorf("nil peer address")
	}

	me, err := pp.codec.Encode(pp.me)
	if err != nil {
		return err
	}
	messageWire := protocol.MessageOnTheWire{
		To:      peerAddr.NetworkAddress(),
		Message: protocol.NewMessage(protocol.V1, protocol.Ping, me),
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case pp.messages <- messageWire:
		return nil
	}
}

func (pp *pingPonger) AcceptPing(ctx context.Context, message protocol.Message) error {
	// TODO: Wrap errors in custom error types.

	// TODO: Check for compatible message version.

	peerAddr, err := pp.codec.Decode(message.Body)
	if err != nil {
		return err
	}
	didUpdate, err := pp.updatePeerAddress(ctx, peerAddr)
	if err != nil {
		return err
	}
	if !didUpdate {
		return nil
	}
	if err := pp.pong(ctx, peerAddr); err != nil {
		return err
	}

	// Propagating the ping will downgrade the ping to the version of this
	// pinger/ponger
	return pp.propagatePing(ctx, message.Body)
}

func (pp *pingPonger) AcceptPong(ctx context.Context, message protocol.Message) error {
	// TODO: Check for compatible message version.

	peerAddr, err := pp.codec.Decode(message.Body)
	if err != nil {
		// TODO: Wrap error in custom error type.

		return err
	}
	if _, err := pp.updatePeerAddress(ctx, peerAddr); err != nil {
		// TODO: Wrap error in custom error type.

		return err
	}
	return nil
}

func (pp *pingPonger) pong(ctx context.Context, to protocol.PeerAddress) error {
	// TODO: Wrap errors in custom error types.

	me, err := pp.codec.Encode(pp.me)
	if err != nil {
		return err
	}
	messageWire := protocol.MessageOnTheWire{
		To:      to.NetworkAddress(),
		Message: protocol.NewMessage(protocol.V1, protocol.Pong, me),
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case pp.messages <- messageWire:
		return nil
	}
}

func (pp *pingPonger) propagatePing(ctx context.Context, body protocol.MessageBody) error {
	// TODO: Wrap errors in custom error types.

	peerAddrs, err := pp.dht.PeerAddresses()
	if err != nil {
		return err
	}
	if peerAddrs == nil {
		return fmt.Errorf("nil peer addresses")
	}
	if len(peerAddrs) <= 0 {
		return fmt.Errorf("empty peer addresses")
	}

	// Using the messaging sending channel protects the pinger/ponger from
	// cascading time outs, but will still capture back pressure
	for i := range peerAddrs {
		messageWire := protocol.MessageOnTheWire{
			To:      peerAddrs[i].NetworkAddress(),
			Message: protocol.NewMessage(protocol.V1, protocol.Ping, body),
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case pp.messages <- messageWire:
		}
	}

	// Return the last error
	return err
}

func (pp *pingPonger) updatePeerAddress(ctx context.Context, peerAddr protocol.PeerAddress) (bool, error) {
	// TODO: Wrap errors in custom error types.

	prevPeerAddr, err := pp.dht.PeerAddress(peerAddr.PeerID())
	if err != nil {
		// FIXME: Double check that the DHT returns nil when an address cannot
		// be found.

		return false, err
	}
	if !peerAddr.IsNewer(prevPeerAddr) {
		return false, nil
	}
	if err := pp.dht.AddPeerAddress(peerAddr); err != nil {
		return false, err
	}

	event := protocol.EventPeerChanged{
		Time:        time.Now(),
		PeerAddress: peerAddr,
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case pp.events <- event:
		return true, nil
	}
}