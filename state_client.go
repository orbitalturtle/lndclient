package lndclient

import (
	"context"
	"fmt"
	"sync"

	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
)

// StateClient exposes base lightning functionality.
type StateClient interface {
	// SubscribeState subscribes to the current state of the wallet.
	SubscribeState(ctx context.Context) (chan WalletState, chan error,
		error)
}

// WalletState is a type that represents all states the lnd wallet can be in.
type WalletState uint8

const (
	// WalletStateNonExisting denotes that no wallet has been created in lnd
	// so far.
	WalletStateNonExisting WalletState = 0

	// WalletStateLocked denotes that a wallet exists in lnd but it has not
	// yet been unlocked.
	WalletStateLocked WalletState = 1

	// WalletStateUnlocked denotes that a wallet exists in lnd and it has
	// been unlocked but the RPC server isn't yet fully started up.
	WalletStateUnlocked WalletState = 2

	// WalletStateRpcActive denotes that lnd is now fully ready to receive
	// RPC requests other than wallet unlocking operations.
	WalletStateRpcActive WalletState = 3
)

// String returns a string representation of the WalletState.
func (s WalletState) String() string {
	switch s {
	case WalletStateNonExisting:
		return "No wallet exists"

	case WalletStateLocked:
		return "Wallet is locked"

	case WalletStateUnlocked:
		return "Wallet is unlocked"

	case WalletStateRpcActive:
		return "Lnd is ready for requests"

	default:
		return fmt.Sprintf("unknown wallet state <%d>", s)
	}
}

// stateClient is a client for lnd's lnrpc.State service.
type stateClient struct {
	client      lnrpc.StateClient
	readonlyMac serializedMacaroon

	wg sync.WaitGroup
}

// newStateClient returns a new stateClient.
func newStateClient(conn *grpc.ClientConn,
	readonlyMac serializedMacaroon) *stateClient {

	return &stateClient{
		client:      lnrpc.NewStateClient(conn),
		readonlyMac: readonlyMac,
	}
}

// WaitForFinished waits until all state subscriptions have finished.
func (s *stateClient) WaitForFinished() {
	s.wg.Wait()
}

// SubscribeState subscribes to the current state of the wallet.
func (s *stateClient) SubscribeState(ctx context.Context) (chan WalletState,
	chan error, error) {

	macaroonAuth := s.readonlyMac.WithMacaroonAuth(ctx)
	resp, err := s.client.SubscribeState(
		macaroonAuth, &lnrpc.SubscribeStateRequest{},
	)
	if err != nil {
		return nil, nil, err
	}

	stateChan := make(chan WalletState, 1)
	errChan := make(chan error, 1)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		for {
			stateEvent, err := resp.Recv()
			if err != nil {
				errChan <- err
				return
			}

			switch stateEvent.State {
			case lnrpc.WalletState_NON_EXISTING:
				stateChan <- WalletStateNonExisting

			case lnrpc.WalletState_LOCKED:
				stateChan <- WalletStateLocked

			case lnrpc.WalletState_UNLOCKED:
				stateChan <- WalletStateUnlocked

			case lnrpc.WalletState_RPC_ACTIVE:
				stateChan <- WalletStateRpcActive

				// This is the final state, no more states will
				// be sent to us.
				close(stateChan)
				close(errChan)

				return

			default:
				errChan <- fmt.Errorf("invalid RPC state: %v",
					stateEvent.State)

				return
			}
		}
	}()

	return stateChan, errChan, nil
}
