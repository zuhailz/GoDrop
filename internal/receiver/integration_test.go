package receiver_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zuhailz/GoDrop/internal/crypto"
	"github.com/zuhailz/GoDrop/internal/host"
	"github.com/zuhailz/GoDrop/internal/protocol"
	"github.com/zuhailz/GoDrop/internal/receiver"
	"github.com/zuhailz/GoDrop/internal/transfer"
)

// testRoomKey is a fixed room key for integration tests.
var testRoomKey = "0123456789ABCDEF0123456789ABCDEF"

func startTestHost(t *testing.T) (*host.Host, int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	roomKeyRaw, err := crypto.ParseRoomKey(testRoomKey)
	if err != nil {
		t.Fatalf("failed to parse test room key: %v", err)
	}

	h, err := host.NewHost(roomKeyRaw, testRoomKey, "test-host")
	if err != nil {
		t.Fatalf("NewHost failed: %v", err)
	}
	h.Port = port

	if err := h.Start(); err != nil {
		t.Fatalf("host Start failed: %v", err)
	}
	t.Cleanup(h.Stop)

	return h, port
}

func TestEndToEndTransfer(t *testing.T) {
	h, port := startTestHost(t)

	r, err := receiver.NewReceiver("peer-1", "peer-one")
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer func() { _ = r.Close() }()

	if err := r.SetRoomKey(testRoomKey); err != nil {
		t.Fatalf("SetRoomKey failed: %v", err)
	}

	if err := r.Connect(fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if peers := h.Peers(); len(peers) != 1 {
		t.Fatalf("expected 1 connected peer, got %d", len(peers))
	}

	srcFile := filepath.Join(t.TempDir(), "payload.bin")
	content := make([]byte, 300*1024)
	rand.New(rand.NewSource(42)).Read(content)
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	if _, err := h.OfferFile(srcFile); err != nil {
		t.Fatalf("OfferFile failed: %v", err)
	}

	var offers []receiver.FileOffer
	deadline := time.After(5 * time.Second)
	for len(offers) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for file offer")
		case offer := <-r.OfferChan():
			offers = append(offers, offer)
		case <-time.After(20 * time.Millisecond):
		}
	}

	destFile := filepath.Join(t.TempDir(), "received.bin")
	if _, _, err := r.AcceptTransfer(offers[0].TransferID, destFile); err != nil {
		t.Fatalf("AcceptTransfer failed: %v", err)
	}

	var completed bool
	deadline = time.After(10 * time.Second)
	for !completed {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for transfer completion")
		case progress := <-r.ProgressChan():
			if progress.State == transfer.StateCompleted {
				completed = true
			}
			if progress.State == transfer.StateFailed {
				t.Fatal("transfer failed on receiver")
			}
		case <-time.After(20 * time.Millisecond):
		}
	}

	received, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("failed to read received file: %v", err)
	}
	if !bytes.Equal(received, content) {
		t.Fatal("received file content does not match source")
	}
}

func TestEndToEndEmptyFileTransfer(t *testing.T) {
	h, port := startTestHost(t)

	r, err := receiver.NewReceiver("peer-empty", "peer-empty")
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer func() { _ = r.Close() }()

	if err := r.SetRoomKey(testRoomKey); err != nil {
		t.Fatalf("SetRoomKey failed: %v", err)
	}

	if err := r.Connect(fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	srcFile := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(srcFile, []byte{}, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	if _, err := h.OfferFile(srcFile); err != nil {
		t.Fatalf("OfferFile failed: %v", err)
	}

	var offer receiver.FileOffer
	select {
	case offer = <-r.OfferChan():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for file offer")
	}

	destFile := filepath.Join(t.TempDir(), "received.bin")
	if _, _, err := r.AcceptTransfer(offer.TransferID, destFile); err != nil {
		t.Fatalf("AcceptTransfer failed: %v", err)
	}

	// Zero-byte files produce no chunks; completion must arrive on its own.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for empty-file transfer completion")
		case progress := <-r.ProgressChan():
			if progress.State == transfer.StateFailed {
				t.Fatal("empty-file transfer failed")
			}
			if progress.State == transfer.StateCompleted {
				info, err := os.Stat(destFile)
				if err != nil {
					t.Fatalf("received file missing: %v", err)
				}
				if info.Size() != 0 {
					t.Fatalf("expected empty file, got %d bytes", info.Size())
				}
				return
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestRejectTransfer(t *testing.T) {
	h, port := startTestHost(t)

	r, err := receiver.NewReceiver("peer-reject", "peer-reject")
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer func() { _ = r.Close() }()

	if err := r.SetRoomKey(testRoomKey); err != nil {
		t.Fatalf("SetRoomKey failed: %v", err)
	}

	if err := r.Connect(fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	srcFile := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(srcFile, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	if _, err := h.OfferFile(srcFile); err != nil {
		t.Fatalf("OfferFile failed: %v", err)
	}

	select {
	case offer := <-r.OfferChan():
		if err := r.RejectTransfer(offer.TransferID); err != nil {
			t.Fatalf("RejectTransfer failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for file offer")
	}
}

func TestConcurrentReceivers(t *testing.T) {
	h, port := startTestHost(t)

	srcFile := filepath.Join(t.TempDir(), "payload.bin")
	content := make([]byte, 100*1024)
	rand.New(rand.NewSource(7)).Read(content)
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	const numReceivers = 3

	connected := make(chan string, numReceivers)
	var wg sync.WaitGroup

	for i := 0; i < numReceivers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			r, err := receiver.NewReceiver(fmt.Sprintf("peer-%d", idx), fmt.Sprintf("peer-name-%d", idx))
			if err != nil {
				t.Errorf("NewReceiver failed: %v", err)
				return
			}
			defer func() { _ = r.Close() }()

			if err := r.SetRoomKey(testRoomKey); err != nil {
				t.Errorf("SetRoomKey failed: %v", err)
				return
			}

			if err := r.Connect(fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
				t.Errorf("Connect failed: %v", err)
				return
			}
			connected <- fmt.Sprintf("peer-%d", idx)

			select {
			case offer := <-r.OfferChan():
				destFile := filepath.Join(t.TempDir(), fmt.Sprintf("received-%d.bin", idx))
				if _, _, err := r.AcceptTransfer(offer.TransferID, destFile); err != nil {
					t.Errorf("AcceptTransfer failed: %v", err)
					return
				}
				// Generous deadline: CI runners are slow, and this test runs
				// under -race with several receivers at once.
				deadline := time.After(30 * time.Second)
				for {
					select {
					case <-deadline:
						t.Errorf("receiver %d timed out", idx)
						return
					case progress := <-r.ProgressChan():
						if progress.State == transfer.StateCompleted {
							received, err := os.ReadFile(destFile)
							if err != nil || !bytes.Equal(received, content) {
								t.Errorf("receiver %d got corrupted file", idx)
								return
							}
							return
						}
						if progress.State == transfer.StateFailed {
							t.Errorf("receiver %d transfer failed", idx)
							return
						}
					case <-time.After(20 * time.Millisecond):
					}
				}
			case <-time.After(10 * time.Second):
				t.Errorf("receiver %d never got offer", idx)
			}
		}(i)
	}

	for i := 0; i < numReceivers; i++ {
		select {
		case <-connected:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for receivers to connect")
		}
	}

	for len(h.Peers()) < numReceivers {
		time.Sleep(50 * time.Millisecond)
	}

	if _, err := h.OfferFile(srcFile); err != nil {
		t.Fatalf("OfferFile failed: %v", err)
	}

	wg.Wait()
}

func TestEndToEndFolderTransfer(t *testing.T) {
	h, port := startTestHost(t)

	r, err := receiver.NewReceiver("peer-folder", "peer-folder")
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer func() { _ = r.Close() }()

	if err := r.SetRoomKey(testRoomKey); err != nil {
		t.Fatalf("SetRoomKey failed: %v", err)
	}

	if err := r.Connect(fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	srcDir := filepath.Join(t.TempDir(), "myfolder")
	subDir := filepath.Join(srcDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create folders: %v", err)
	}

	fileA := []byte("alpha content here")
	fileB := []byte("beta content here, a bit longer")
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), fileA, 0644); err != nil {
		t.Fatalf("failed to write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), fileB, 0644); err != nil {
		t.Fatalf("failed to write b.txt: %v", err)
	}

	if _, err := h.OfferFile(srcDir); err != nil {
		t.Fatalf("OfferFile failed: %v", err)
	}

	var offer receiver.FileOffer
	select {
	case offer = <-r.OfferChan():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for folder offer")
	}

	if !offer.Folder {
		t.Fatalf("expected folder offer, got Folder=false")
	}

	saveDir := t.TempDir()
	if _, _, err := r.AcceptTransfer(offer.TransferID, filepath.Join(saveDir, offer.Filename)); err != nil {
		t.Fatalf("AcceptTransfer failed: %v", err)
	}

	deadline := time.After(10 * time.Second)
first:
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for folder transfer")
		case progress := <-r.ProgressChan():
			if progress.State == transfer.StateFailed {
				t.Fatal("folder transfer failed")
			}
			if progress.State != transfer.StateCompleted {
				continue
			}

			gotA, err := os.ReadFile(filepath.Join(saveDir, "myfolder", "a.txt"))
			if err != nil || !bytes.Equal(gotA, fileA) {
				t.Fatalf("a.txt mismatch: %v", err)
			}
			gotB, err := os.ReadFile(filepath.Join(saveDir, "myfolder", "sub", "b.txt"))
			if err != nil || !bytes.Equal(gotB, fileB) {
				t.Fatalf("b.txt mismatch: %v", err)
			}

			if _, err := os.Stat(filepath.Join(saveDir, "myfolder.zip")); err == nil {
				t.Fatal("temp archive should be removed, still exists")
			}

			if _, err := os.Stat(filepath.Join(saveDir, "myfolder(1)")); err == nil {
				t.Fatal("myfolder(1) should not exist yet")
			}
			break first
		}
	}

	if _, err := h.OfferFile(srcDir); err != nil {
		t.Fatalf("second OfferFile failed: %v", err)
	}

	var secondOffer receiver.FileOffer
	select {
	case secondOffer = <-r.OfferChan():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second folder offer")
	}

	gotPath, renamed, err := r.AcceptTransfer(secondOffer.TransferID, filepath.Join(saveDir, secondOffer.Filename))
	if err != nil {
		t.Fatalf("second AcceptTransfer failed: %v", err)
	}
	if !renamed {
		t.Fatalf("expected rename on duplicate folder, got path %s", gotPath)
	}

	deadline = time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for second folder transfer")
		case progress := <-r.ProgressChan():
			if progress.State == transfer.StateFailed {
				t.Fatal("second folder transfer failed")
			}
			if progress.State != transfer.StateCompleted {
				continue
			}

			gotA, err := os.ReadFile(filepath.Join(saveDir, "myfolder(1)", "a.txt"))
			if err != nil || !bytes.Equal(gotA, fileA) {
				t.Fatalf("myfolder(1)/a.txt mismatch: %v", err)
			}
			gotB, err := os.ReadFile(filepath.Join(saveDir, "myfolder(1)", "sub", "b.txt"))
			if err != nil || !bytes.Equal(gotB, fileB) {
				t.Fatalf("myfolder(1)/sub/b.txt mismatch: %v", err)
			}
			return
		}
	}
}

func TestSystemEventBroadcast(t *testing.T) {
	h, port := startTestHost(t)

	newReceiver := func(idx int) *receiver.Receiver {
		r, err := receiver.NewReceiver(fmt.Sprintf("peer-%d", idx), fmt.Sprintf("peer-%d", idx))
		if err != nil {
			t.Fatalf("NewReceiver failed: %v", err)
		}
		t.Cleanup(func() { _ = r.Close() })
		if err := r.SetRoomKey(testRoomKey); err != nil {
			t.Fatalf("SetRoomKey failed: %v", err)
		}
		if err := r.Connect(fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		return r
	}

	waitForEvent := func(ch <-chan protocol.SystemEvent, wantSubstring string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case <-deadline:
				t.Fatalf("timed out waiting for system event containing %q", wantSubstring)
			case event := <-ch:
				if strings.Contains(event.Text, wantSubstring) {
					return
				}
			}
		}
	}

	r1 := newReceiver(1)
	waitForEvent(h.SystemEventChan(), "peer-1 connected")
	waitForEvent(r1.SystemEventChan(), "peer-1 connected")

	r2 := newReceiver(2)
	time.Sleep(200 * time.Millisecond)
	waitForEvent(r1.SystemEventChan(), "peer-2 connected")

	srcFile := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(srcFile, []byte("system-event payload"), 0644); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	if _, err := h.OfferFile(srcFile); err != nil {
		t.Fatalf("OfferFile failed: %v", err)
	}

	waitForEvent(h.SystemEventChan(), "offered payload.bin")
	waitForEvent(r2.SystemEventChan(), "offered payload.bin")

	var offer receiver.FileOffer
	select {
	case offer = <-r1.OfferChan():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for offer on r1")
	}

	destFile := filepath.Join(t.TempDir(), "received.bin")
	if _, _, err := r1.AcceptTransfer(offer.TransferID, destFile); err != nil {
		t.Fatalf("AcceptTransfer failed: %v", err)
	}

	waitForEvent(h.SystemEventChan(), "peer-1 accepted payload.bin")
	waitForEvent(r2.SystemEventChan(), "peer-1 accepted payload.bin")

	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for transfer completion")
		case progress := <-r1.ProgressChan():
			if progress.State == transfer.StateFailed {
				t.Fatal("transfer failed")
			}
			if progress.State == transfer.StateCompleted {
				goto completed
			}
		case <-time.After(20 * time.Millisecond):
		}
	}

completed:
	waitForEvent(h.SystemEventChan(), "peer-1 received payload.bin")
	waitForEvent(r2.SystemEventChan(), "peer-1 received payload.bin")
}

func TestWrongRoomKeyRejected(t *testing.T) {
	h, port := startTestHost(t)

	r, err := receiver.NewReceiver("peer-wrong", "peer-wrong")
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}
	defer func() { _ = r.Close() }()

	// Wrong room key: the handshake has to fail at the auth stage.
	wrongKey := "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"
	if err := r.SetRoomKey(wrongKey); err != nil {
		t.Fatalf("SetRoomKey failed: %v", err)
	}

	err = r.Connect(fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		t.Fatal("expected connection to fail with wrong room key, but it succeeded")
	}

	// No peer should be registered on the host.
	peers := h.Peers()
	for _, p := range peers {
		if p == "peer-wrong" {
			t.Error("wrong-key peer should not have registered on host")
		}
	}
}

// startImpostorHost runs a fake host that replays the real handshake until
// it has to prove knowledge of the room key, then sends whatever tag
// forgeHostTag produces. Without the key no honest host tag is computable,
// so reflected and forged tags are the best an impostor can do.
func startImpostorHost(t *testing.T, forgeHostTag func(receiverTag []byte) []byte) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Take the victim's key exchange without acting on it.
		if _, _, err := protocol.Unmarshal(conn); err != nil {
			return
		}

		// Pose as the host with our own ephemeral key.
		keyPair, err := crypto.GenerateKeyPair()
		if err != nil {
			return
		}
		fakeHello := protocol.KeyExchange{PublicKey: crypto.MarshalPublicKey(keyPair.PublicKey)}
		if err := protocol.Marshal(conn, protocol.MsgKeyExchange, fakeHello); err != nil {
			return
		}

		// Demand proof of room-key knowledge, exactly like the real host.
		if err := protocol.Marshal(conn, protocol.MsgPinChallenge, protocol.PinChallenge{}); err != nil {
			return
		}

		// Take the receiver's proof-of-knowledge tag.
		_, rawTagMsg, err := protocol.Unmarshal(conn)
		if err != nil {
			return
		}
		var resp protocol.PinResponse
		if err := json.Unmarshal(rawTagMsg, &resp); err != nil {
			return
		}

		// Answer with our best attempt at a host tag. Without the room
		// key there is no way to compute a valid one.
		forged := protocol.PinResponse{Tag: forgeHostTag(resp.Tag)}
		_ = protocol.Marshal(conn, protocol.MsgPinResponse, forged)
	}()

	return ln.Addr().(*net.TCPAddr).Port
}

// TestImpostorHostRejected locks in the receiver half of the mutual-auth
// design: a host reached by direct IP that cannot prove room-key knowledge
// must be rejected regardless of what tag it sends back. Reflection of the
// receiver's own tag is the classic attack this guards against.
func TestImpostorHostRejected(t *testing.T) {
	cases := []struct {
		name  string
		forge func(receiverTag []byte) []byte
	}{
		{
			// Relay attack: echo the receiver's tag back as ours. Role
			// separation must make this worthless.
			name:  "reflected-receiver-tag",
			forge: func(tag []byte) []byte { return tag },
		},
		{
			// Bluff: fabricate bytes and hope verification is missing.
			name: "fabricated-tag",
			forge: func([]byte) []byte {
				tag := make([]byte, 32)
				for i := range tag {
					tag[i] = byte(i ^ 0xA5)
				}
				return tag
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := startImpostorHost(t, tc.forge)

			r, err := receiver.NewReceiver("peer-victim", "victim")
			if err != nil {
				t.Fatalf("NewReceiver failed: %v", err)
			}
			defer func() { _ = r.Close() }()

			if err := r.SetRoomKey(testRoomKey); err != nil {
				t.Fatalf("SetRoomKey failed: %v", err)
			}

			err = r.Connect(fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				t.Fatal("receiver accepted an impostor host")
			}
			// The error has to come from the auth stage specifically. That
			// proves the tag was actually checked, not just that the socket
			// broke.
			if !strings.Contains(err.Error(), "verification failed") {
				t.Fatalf("expected room-key verification failure, got: %v", err)
			}
		})
	}
}
