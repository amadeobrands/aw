package broadcast_test

import (
	"bytes"
	"context"
	"testing/quick"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/renproject/aw/broadcast"
	. "github.com/renproject/aw/testutil"

	"github.com/renproject/aw/protocol"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Broadcaster", func() {

	Context("when broadcasting", func() {
		It("should be able to send messages", func() {
			check := func(messageBody []byte) bool {
				messages := make(chan protocol.MessageOnTheWire, 128)
				events := make(chan protocol.Event, 1)
				dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
				broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

				groupID, addrs, err := NewGroup(dht)
				Expect(err).NotTo(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				Expect(broadcaster.Broadcast(ctx, groupID, messageBody)).NotTo(HaveOccurred())

				for i := 0; i < len(addrs); i++ {
					var message protocol.MessageOnTheWire
					Eventually(messages).Should(Receive(&message))
					Expect(addrs).Should(ContainElement(message.To))
					Expect(message.Message.Version).Should(Equal(protocol.V1))
					Expect(message.Message.Variant).Should(Equal(protocol.Broadcast))
					Expect(bytes.Equal(message.Message.Body, messageBody)).Should(BeTrue())
				}
				return true
			}

			Expect(quick.Check(check, nil)).Should(BeNil())
		})

		It("should not broadcast the same message more than once", func() {
			check := func(messageBody []byte) bool {
				messages := make(chan protocol.MessageOnTheWire, 128)
				events := make(chan protocol.Event, 1)
				dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
				broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

				groupID, addrs, err := NewGroup(dht)
				Expect(err).NotTo(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				Expect(broadcaster.Broadcast(ctx, groupID, messageBody)).NotTo(HaveOccurred())

				for i := 0; i < len(addrs); i++ {
					var message protocol.MessageOnTheWire
					Eventually(messages).Should(Receive(&message))
					Expect(addrs).Should(ContainElement(message.To))
					Expect(message.Message.Version).Should(Equal(protocol.V1))
					Expect(message.Message.Variant).Should(Equal(protocol.Broadcast))
					Expect(bytes.Equal(message.Message.Body, messageBody)).Should(BeTrue())
				}

				Expect(broadcaster.Broadcast(ctx, groupID, messageBody)).NotTo(HaveOccurred())
				var message protocol.MessageOnTheWire
				Eventually(messages).ShouldNot(Receive(&message))
				return true
			}

			Expect(quick.Check(check, nil)).Should(BeNil())
		})

		Context("when the context is cancelled", func() {
			It("should return ErrBroadcasting", func() {
				check := func(messageBody []byte) bool {
					messages := make(chan protocol.MessageOnTheWire, 128)
					events := make(chan protocol.Event, 1)
					dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
					broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

					groupID, _, err := NewGroup(dht)
					Expect(err).NotTo(HaveOccurred())

					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					Expect(broadcaster.Broadcast(ctx, groupID, messageBody)).To(HaveOccurred())
					return true
				}

				Expect(quick.Check(check, nil)).Should(BeNil())
			})
		})

		Context("when some of the addresses cannot be found from the store", func() {
			It("should skip the nodes which we don't have the addresses", func() {
				check := func(messageBody []byte) bool {
					messages := make(chan protocol.MessageOnTheWire, 128)
					events := make(chan protocol.Event, 1)
					dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
					broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

					groupID, addrs, err := NewGroup(dht)
					Expect(err).NotTo(HaveOccurred())
					if addrs[0].PeerID().Equal(dht.Me().PeerID()) {
						return true
					}
					Expect(dht.RemovePeerAddress(addrs[0].PeerID())).NotTo(HaveOccurred())

					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					Expect(broadcaster.Broadcast(ctx, groupID, messageBody)).NotTo(HaveOccurred())

					for i := 0; i < len(addrs)-1; i++ {
						var message protocol.MessageOnTheWire
						Eventually(messages).Should(Receive(&message))
						Expect(addrs).Should(ContainElement(message.To))
						Expect(message.Message.Version).Should(Equal(protocol.V1))
						Expect(message.Message.Variant).Should(Equal(protocol.Broadcast))
						Expect(bytes.Equal(message.Message.Body, messageBody)).Should(BeTrue())
					}
					Eventually(messages).ShouldNot(Receive())

					return true
				}

				Expect(quick.Check(check, nil)).Should(BeNil())
			})
		})
	})

	Context("when accepting broadcasts", func() {
		It("should be able to receive messages", func() {
			check := func(messageBody []byte) bool {
				messages := make(chan protocol.MessageOnTheWire, 128)
				events := make(chan protocol.Event, 16)
				dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
				broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

				groupID, addrs, err := NewGroup(dht)
				Expect(err).NotTo(HaveOccurred())

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				message := protocol.NewMessage(protocol.V1, protocol.Broadcast, groupID, messageBody)
				Expect(broadcaster.AcceptBroadcast(ctx, RandomPeerID(), message)).ToNot(HaveOccurred())

				var event protocol.EventMessageReceived
				Eventually(events).Should(Receive(&event))
				Expect(bytes.Equal(event.Message, messageBody)).Should(BeTrue())

				for range addrs {
					var message protocol.MessageOnTheWire
					Eventually(messages).Should(Receive(&message))
					Expect(message.Message.Version).Should(Equal(protocol.V1))
					Expect(message.Message.Variant).Should(Equal(protocol.Broadcast))
					Expect(bytes.Equal(message.Message.Body, messageBody)).Should(BeTrue())
					Expect(addrs).Should(ContainElement(message.To))
				}
				return true
			}

			Expect(quick.Check(check, nil)).Should(BeNil())
		})

		Context("when the context is cancelled", func() {
			It("should return ErrAcceptingBroadcast", func() {
				check := func(messageBody []byte) bool {
					messages := make(chan protocol.MessageOnTheWire, 128)
					events := make(chan protocol.Event, 16)
					dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
					broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					message := protocol.NewMessage(protocol.V1, protocol.Broadcast, RandomGroupID(), messageBody)
					Expect(broadcaster.AcceptBroadcast(ctx, RandomPeerID(), message)).To(HaveOccurred())

					return true
				}

				Expect(quick.Check(check, nil)).Should(BeNil())
			})
		})

		Context("when the message has an unsupported version", func() {
			It("should return ErrBroadcastVersionNotSupported", func() {
				check := func(messageBody []byte) bool {
					messages := make(chan protocol.MessageOnTheWire, 128)
					events := make(chan protocol.Event, 16)
					dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
					broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					message := protocol.NewMessage(protocol.V1, protocol.Broadcast, RandomGroupID(), messageBody)
					message.Version = InvalidMessageVersion()
					Expect(broadcaster.AcceptBroadcast(ctx, RandomPeerID(), message)).To(HaveOccurred())

					return true
				}

				Expect(quick.Check(check, nil)).Should(BeNil())
			})
		})

		Context("when the message has an unsupported variant", func() {
			It("should return ErrBroadcastVariantNotSupported", func() {
				check := func(messageBody []byte) bool {
					messages := make(chan protocol.MessageOnTheWire, 128)
					events := make(chan protocol.Event, 16)
					dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
					broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					message := protocol.NewMessage(protocol.V1, protocol.Broadcast, RandomGroupID(), messageBody)
					message.Variant = InvalidMessageVariant(protocol.Broadcast)
					Expect(broadcaster.AcceptBroadcast(ctx, RandomPeerID(), message)).To(HaveOccurred())

					return true
				}

				Expect(quick.Check(check, nil)).Should(BeNil())
			})
		})

		Context("when receive the same message more than once", func() {
			It("should only broadcast the same message once", func() {
				check := func(messageBody []byte) bool {
					messages := make(chan protocol.MessageOnTheWire, 128)
					events := make(chan protocol.Event, 16)
					dht := NewDHT(RandomAddress(), NewTable("dht"), nil)
					broadcaster := NewBroadcaster(logrus.New(), 8, messages, events, dht)

					groupID, addrs, err := NewGroup(dht)
					Expect(err).NotTo(HaveOccurred())

					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					message := protocol.NewMessage(protocol.V1, protocol.Broadcast, groupID, messageBody)
					Expect(broadcaster.AcceptBroadcast(ctx, RandomPeerID(), message)).NotTo(HaveOccurred())

					var event protocol.EventMessageReceived
					Eventually(events).Should(Receive(&event))
					Expect(bytes.Equal(event.Message, messageBody)).Should(BeTrue())

					for range addrs {
						var message protocol.MessageOnTheWire
						Eventually(messages).Should(Receive(&message))
						Expect(message.Message.Version).Should(Equal(protocol.V1))
						Expect(message.Message.Variant).Should(Equal(protocol.Broadcast))
						Expect(bytes.Equal(message.Message.Body, messageBody)).Should(BeTrue())
						Expect(addrs).Should(ContainElement(message.To))
					}

					Expect(broadcaster.AcceptBroadcast(ctx, RandomPeerID(), message)).ToNot(HaveOccurred())
					Eventually(events).ShouldNot(Receive())
					return true
				}

				Expect(quick.Check(check, nil)).Should(BeNil())
			})
		})
	})
})
