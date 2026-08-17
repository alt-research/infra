package proxyd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

func TestFetchELStateFastChainTagOrder(t *testing.T) {
	var calls []string
	backendServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode RPC request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		var params []json.RawMessage
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Errorf("decode RPC params: %v", err)
			http.Error(w, "invalid params", http.StatusBadRequest)
			return
		}
		if len(params) == 0 {
			t.Error("RPC params are empty")
			http.Error(w, "empty params", http.StatusBadRequest)
			return
		}

		var tag string
		if err := json.Unmarshal(params[0], &tag); err != nil {
			t.Errorf("decode block tag: %v", err)
			http.Error(w, "invalid block tag", http.StatusBadRequest)
			return
		}
		calls = append(calls, tag)

		blockNumber := uint64(100)
		if len(calls) == 3 {
			blockNumber++
		}

		res := rpcResJSON{
			JSONRPC: JSONRPCVersion,
			ID:      req.ID,
			Result: map[string]interface{}{
				"number": hexutil.EncodeUint64(blockNumber),
				"hash":   fmt.Sprintf("0x%064x", blockNumber),
			},
		}
		if err := json.NewEncoder(w).Encode(res); err != nil {
			t.Errorf("encode RPC response: %v", err)
		}
	}))
	defer backendServer.Close()

	backend := NewBackend("fast-chain", backendServer.URL, "", nil)
	poller := &ConsensusPoller{}
	state, err := poller.fetchELState(context.Background(), backend)
	require.NoError(t, err)

	require.Equal(t, []string{"finalized", "safe", "latest"}, calls)
	require.True(t, poller.checkExpectedBlockTags(
		0,
		0,
		state.LatestBlockNumber,
		0,
		state.SafeBlockNumber,
		0,
		state.FinalizedBlockNumber,
	))
}

func TestFindConsensusBlock_exhaustsAtGenesis(t *testing.T) {
	ctx := context.Background()
	cp := &ConsensusPoller{}

	be1 := &Backend{Name: "node-a"}
	be2 := &Backend{Name: "node-b"}
	candidates := map[*Backend]*backendState{
		be1: {},
		be2: {},
	}

	fetch := func(ctx context.Context, be *Backend, block hexutil.Uint64) (hexutil.Uint64, string, error) {
		if be == be1 {
			return block, "hash-a", nil
		}
		return block, "hash-b", nil
	}

	num, hash, broken := cp.findConsensusBlock(ctx, candidates, 0, 2, "hash-a", fetch, "test")
	require.Equal(t, hexutil.Uint64(0), num)
	require.Equal(t, "", hash)
	require.True(t, broken)
}

func TestFindConsensusBlock_agreesAtBlockZero(t *testing.T) {
	ctx := context.Background()
	cp := &ConsensusPoller{}

	be1 := &Backend{Name: "node-a"}
	be2 := &Backend{Name: "node-b"}
	candidates := map[*Backend]*backendState{
		be1: {},
		be2: {},
	}

	fetch := func(ctx context.Context, be *Backend, block hexutil.Uint64) (hexutil.Uint64, string, error) {
		switch uint64(block) {
		case 1:
			if be == be1 {
				return block, "hash-a-at-1", nil
			}
			return block, "hash-b-at-1", nil
		case 0:
			return block, "genesis-shared", nil
		default:
			return block, "unused", nil
		}
	}

	num, hash, broken := cp.findConsensusBlock(ctx, candidates, 0, 1, "hash-a-at-1", fetch, "test")
	require.Equal(t, hexutil.Uint64(0), num)
	require.Equal(t, "genesis-shared", hash)
	require.False(t, broken)
}
