package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
	"github.com/zuhailz/GoDrop/internal/crypto"
	"github.com/zuhailz/GoDrop/internal/host"
	"github.com/zuhailz/GoDrop/internal/receiver"
	hostTUI "github.com/zuhailz/GoDrop/internal/tui/host"
	receiverTUI "github.com/zuhailz/GoDrop/internal/tui/receiver"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "godrop",
	Short: "Secure P2P file transfer over local network",
	Long:  "GoDrop is a secure peer-to-peer file transfer tool using mDNS discovery and ECDH encryption.",
}

var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "Start a host and wait for receivers to connect",
	RunE:  runHost,
}

var connectCmd = &cobra.Command{
	Use:   "connect <room-key>",
	Short: "Connect to a host by room key",
	Args:  cobra.ExactArgs(1),
	RunE:  runConnect,
}

var (
	hostName  string
	peerName  string
	connectIP string
	saveDir   string
)

func init() {
	rootCmd.AddCommand(hostCmd)
	rootCmd.AddCommand(connectCmd)

	hostCmd.Flags().StringVarP(&hostName, "name", "n", "", "Host display name")
	connectCmd.Flags().StringVarP(&peerName, "name", "n", "", "Peer display name")
	connectCmd.Flags().StringVarP(&connectIP, "ip", "i", "", "Direct IP:port to connect (skips mDNS discovery)")
	connectCmd.Flags().StringVarP(&saveDir, "save-dir", "s", ".", "Directory to save received files")
	rootCmd.Version = resolvedVersion()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runHost(cmd *cobra.Command, args []string) error {
	if hostName == "" {
		generated, err := generateName("host")
		if err != nil {
			return err
		}
		hostName = generated
	}

	roomKeyRaw, err := crypto.GenerateRoomKey()
	if err != nil {
		return err
	}
	roomKey := crypto.FormatRoomKey(roomKeyRaw)

	h, err := host.NewHost(roomKeyRaw, roomKey, hostName)
	if err != nil {
		return fmt.Errorf("failed to create host: %w", err)
	}

	if err := h.Start(); err != nil {
		return fmt.Errorf("failed to start host: %w", err)
	}
	defer h.Stop()

	return hostTUI.Run(h)
}

func runConnect(cmd *cobra.Command, args []string) error {
	roomKey := args[0]

	if peerName == "" {
		generated, err := generateName("peer")
		if err != nil {
			return err
		}
		peerName = generated
	}

	peerID, err := generatePeerID()
	if err != nil {
		return err
	}

	r, err := receiver.NewReceiver(peerID, peerName)
	if err != nil {
		return fmt.Errorf("failed to create receiver: %w", err)
	}

	return receiverTUI.Run(r, saveDir, roomKey, connectIP)
}

func generatePeerID() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate peer ID: %w", err)
	}
	return "peer-" + hex.EncodeToString(bytes), nil
}

func generateName(prefix string) (string, error) {
	bytes := make([]byte, 2)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate display name: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(bytes), nil
}

// resolvedVersion reports how this binary was built. A value set at link time
// (-ldflags "-X main.version=...") always wins. Binaries installed through the
// Go module proxy carry their module version in BuildInfo; plain source builds
// fall back to the development placeholder.
func resolvedVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
