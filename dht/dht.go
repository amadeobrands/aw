package dht

import (
	"fmt"
	"math/rand"
	"sync"

	"github.com/renproject/aw/protocol"
	"github.com/renproject/kv"
)

// A DHT is a distributed hash table. It is used for storing peer addresses. A
// DHT is not required to be persistent and will often purge stale peer
// addresses.
type DHT interface {

	// Me returns self PeerAddress
	Me() protocol.PeerAddress

	// NumPeers returns total number of PeerAddresses stored in the DHT.
	NumPeers() (int, error)

	// PeerAddress returns the resolved protocol.PeerAddress of the given
	// PeerID. It returns an ErrPeerNotFound if the PeerID cannot be found.
	PeerAddress(protocol.PeerID) (protocol.PeerAddress, error)

	// PeerAddresses returns all the PeerAddresses stored in the DHT.
	PeerAddresses() (protocol.PeerAddresses, error)

	// RandomPeerAddresses returns (at max) n random PeerAddresses in the given
	// peer group.
	RandomPeerAddresses(id protocol.GroupID, n int) (protocol.PeerAddresses, error)

	// AddPeerAddress adds a PeerAddress into the DHT.
	AddPeerAddress(protocol.PeerAddress) error

	// UpdatePeerAddress tries to update the PeerAddress in the DHT. It returns
	// true if the given peerAddr is newer than the one we stored.
	UpdatePeerAddress(protocol.PeerAddress) (bool, error)

	// RemovePeerAddress removes the PeerAddress of given PeerID from the DHT.
	// It wouldn't return any error if the PeerAddress doesn't exist.
	RemovePeerAddress(protocol.PeerID) error

	// AddGroup creates a new group in the DHT with given ID and PeerIDs.
	AddGroup(protocol.GroupID, protocol.PeerIDs) error

	// GroupIDs returns the PeerIDs in the group with the given ID.
	GroupIDs(protocol.GroupID) (protocol.PeerIDs, error)

	// GroupAddresses returns the PeerAddresses in the group with the given ID.
	// It will not return peers for which we do not have the PeerAddresses.
	GroupAddresses(protocol.GroupID) (protocol.PeerAddresses, error)

	// Remove a group from the DHT with the given ID.
	RemoveGroup(protocol.GroupID)
}

type dht struct {
	me    protocol.PeerAddress
	codec protocol.PeerAddressCodec
	store kv.Table

	groupsMu *sync.RWMutex
	groups   map[protocol.GroupID]protocol.PeerIDs

	inMemCacheMu *sync.RWMutex
	inMemCache   map[string]protocol.PeerAddress
}

// New DHT that stores peer addresses in the given store. It will cache all
// peer addresses in memory for fast access. It is safe for concurrent use,
// regardless of the underlying store.
func New(me protocol.PeerAddress, codec protocol.PeerAddressCodec, store kv.Table, bootstrapAddrs ...protocol.PeerAddress) (DHT, error) {
	// Validate input parameters
	if me == nil {
		panic("pre-condition violation: self PeerAddress cannot be nil")
	}
	if codec == nil {
		panic("pre-condition violation: PeerAddressCodec cannot be nil")
	}

	// Create a in-memory store if user doesn't provide one.
	if store == nil {
		store = kv.NewTable(kv.NewMemDB(kv.GobCodec), "dht")
	}

	dht := &dht{
		me:    me,
		codec: codec,
		store: store,

		groupsMu: new(sync.RWMutex),
		groups:   map[protocol.GroupID]protocol.PeerIDs{},

		inMemCacheMu: new(sync.RWMutex),
		inMemCache:   map[string]protocol.PeerAddress{},
	}

	if err := dht.fillInMemCache(); err != nil {
		return nil, err
	}
	return dht, dht.addBootstrapNodes(bootstrapAddrs)
}

func (dht *dht) Me() protocol.PeerAddress {
	return dht.me
}

func (dht *dht) NumPeers() (int, error) {
	dht.inMemCacheMu.RLock()
	defer dht.inMemCacheMu.RUnlock()

	return len(dht.inMemCache), nil
}

func (dht *dht) PeerAddresses() (protocol.PeerAddresses, error) {
	dht.inMemCacheMu.RLock()
	defer dht.inMemCacheMu.RUnlock()

	peerAddrs := make(protocol.PeerAddresses, 0, len(dht.inMemCache))
	for _, peerAddr := range dht.inMemCache {
		peerAddrs = append(peerAddrs, peerAddr)
	}

	return peerAddrs, nil
}

func (dht *dht) RandomPeerAddresses(groupID protocol.GroupID, n int) (protocol.PeerAddresses, error) {
	addrs, err := dht.GroupAddresses(groupID)
	if err != nil {
		return nil, err
	}
	if len(addrs) < n {
		n = len(addrs)
	}

	indexes := rand.Perm(len(addrs))
	randAddrs := make(protocol.PeerAddresses, n)
	for i := range randAddrs {
		randAddrs[i] = addrs[indexes[i]]
	}
	return randAddrs, nil
}

func (dht *dht) AddPeerAddress(peerAddr protocol.PeerAddress) error {
	dht.inMemCacheMu.Lock()
	defer dht.inMemCacheMu.Unlock()

	return dht.addPeerAddressWithoutLock(peerAddr)
}

func (dht *dht) PeerAddress(id protocol.PeerID) (protocol.PeerAddress, error) {
	dht.inMemCacheMu.RLock()
	defer dht.inMemCacheMu.RUnlock()

	peerAddr, ok := dht.inMemCache[id.String()]
	if !ok {
		return nil, NewErrPeerNotFound(id)
	}
	return peerAddr, nil
}

func (dht *dht) UpdatePeerAddress(peerAddr protocol.PeerAddress) (bool, error) {
	dht.inMemCacheMu.Lock()
	defer dht.inMemCacheMu.Unlock()

	prevPeerAddr, ok := dht.inMemCache[peerAddr.PeerID().String()]
	if ok && !peerAddr.IsNewer(prevPeerAddr) {
		return false, nil
	}

	err := dht.addPeerAddressWithoutLock(peerAddr)
	return err == nil, err
}

func (dht *dht) RemovePeerAddress(id protocol.PeerID) error {
	dht.inMemCacheMu.Lock()
	defer dht.inMemCacheMu.Unlock()

	if err := dht.store.Delete(id.String()); err != nil {
		return fmt.Errorf("error deleting peer=%v from dht: %v", id, err)
	}

	delete(dht.inMemCache, id.String())
	return nil
}

func (dht *dht) AddGroup(id protocol.GroupID, ids protocol.PeerIDs) error {
	if id.Equal(protocol.NilGroupID) {
		return protocol.ErrInvalidGroupID
	}

	dht.groupsMu.Lock()
	defer dht.groupsMu.Unlock()

	dht.groups[id] = ids
	return nil
}

func (dht *dht) GroupIDs(groupID protocol.GroupID) (protocol.PeerIDs, error) {
	if groupID.Equal(protocol.NilGroupID) {
		addrs, err := dht.PeerAddresses()
		if err != nil {
			return nil, err
		}
		ids := make([]protocol.PeerID, len(addrs))
		for i := range ids {
			ids[i] = addrs[i].PeerID()
		}
		return ids, nil
	}

	dht.groupsMu.RLock()
	defer dht.groupsMu.RUnlock()

	peerIDs, ok := dht.groups[groupID]
	if !ok {
		return nil, NewErrGroupNotFound(groupID)
	}
	peerIDsCopy := make([]protocol.PeerID, len(peerIDs))
	copy(peerIDsCopy, peerIDs)
	return peerIDsCopy, nil
}

func (dht *dht) GroupAddresses(groupID protocol.GroupID) (protocol.PeerAddresses, error) {
	if groupID.Equal(protocol.NilGroupID) {
		return dht.PeerAddresses()
	}

	ids, err := dht.GroupIDs(groupID)
	if err != nil {
		return nil, err
	}
	addrs := make([]protocol.PeerAddress, 0, len(ids))
	dht.inMemCacheMu.RLock()
	defer dht.inMemCacheMu.RUnlock()
	for _, id := range ids {
		if id.Equal(dht.me.PeerID()) {
			addrs = append(addrs, dht.me)
			continue
		}
		addr, ok := dht.inMemCache[id.String()]
		if !ok {
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func (dht *dht) RemoveGroup(id protocol.GroupID) {
	dht.groupsMu.Lock()
	defer dht.groupsMu.Unlock()

	delete(dht.groups, id)
}

func (dht *dht) addPeerAddressWithoutLock(peerAddr protocol.PeerAddress) error {
	data, err := dht.codec.Encode(peerAddr)
	if err != nil {
		return fmt.Errorf("error encoding peer address=%v: %v", peerAddr, err)
	}
	if err := dht.store.Insert(peerAddr.PeerID().String(), data); err != nil {
		return fmt.Errorf("error inserting peer address=%v into dht: %v", peerAddr, err)
	}
	dht.inMemCache[peerAddr.PeerID().String()] = peerAddr
	return nil
}

func (dht *dht) fillInMemCache() error {
	iter := dht.store.Iterator()
	defer iter.Close()

	for iter.Next() {
		var data []byte
		if err := iter.Value(&data); err != nil {
			return fmt.Errorf("error scanning dht iterator: %v", err)
		}
		peerAddr, err := dht.codec.Decode(data)
		if err != nil {
			return fmt.Errorf("error decoding peerAddress: %v", err)
		}
		dht.inMemCache[peerAddr.PeerID().String()] = peerAddr
	}
	return nil
}

// addBootstrapNodes loops through all the bootstrap nodes, update the store if
// it is newer than the stored addresses.
func (dht *dht) addBootstrapNodes(addrs protocol.PeerAddresses) error {
	for _, addr := range addrs {
		if addr.PeerID().Equal(dht.Me().PeerID()) {
			continue
		}
		if _, err := dht.UpdatePeerAddress(addr); err != nil {
			return err
		}
	}
	return nil
}

type ErrPeerNotFound struct {
	error
	protocol.PeerID
}

func NewErrPeerNotFound(peerID protocol.PeerID) error {
	return ErrPeerNotFound{
		error:  fmt.Errorf("peer=%v not found", peerID),
		PeerID: peerID,
	}
}

type ErrGroupNotFound struct {
	error
	protocol.GroupID
}

func NewErrGroupNotFound(groupID protocol.GroupID) error {
	return ErrGroupNotFound{
		error:   fmt.Errorf("peer group=%v not found", groupID),
		GroupID: groupID,
	}
}
