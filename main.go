package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	crypto "github.com/libp2p/go-libp2p-crypto"
	routing "github.com/libp2p/go-libp2p-routing"
	"github.com/libp2p/go-libp2p/p2p/discovery"
	"github.com/multiformats/go-multiaddr"
	"github.com/sirupsen/logrus"

	"github.com/libp2p/go-libp2p-core/host"
	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"

	"github.com/kirsle/configdir"
)

// DiscoveryInterval is how often we re-publish our mDNS records.
const DiscoveryInterval = time.Hour

// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
const DiscoveryServiceTag = "pubsub-chat-example"

// bootstrappers
type arrayFlags []string

func (i *arrayFlags) String() string {
	return "my string representation"
}
func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

var bootstrappers arrayFlags

func getKey() crypto.PrivKey {
	keyfile := configdir.LocalConfig("chatp2p", ".key")

	// open private key file
	content, err := ioutil.ReadFile(keyfile)
	if err != nil {
		panic(err)
	}

	hexString := strings.TrimSuffix(string(content), "\n")
	decoded, err := hex.DecodeString(hexString)
	if err != nil {
		panic(err)
	}

	privNew, err := crypto.UnmarshalPrivateKey(decoded)
	if err != nil {
		panic(err)
	}
	return privNew
}

func createKey() {
	keyfile := configdir.LocalConfig("chatp2p", ".key")
	// Create a new ECDSA key pair for this host.
	prvKey, _, err := crypto.GenerateECDSAKeyPair(rand.Reader)
	if err != nil {
		panic(err)
	}
	privB, err := prvKey.Bytes()
	if err != nil {
		panic(err)
	}
	pk := fmt.Sprintf("%x", string(privB))
	ioutil.WriteFile(keyfile, []byte(pk), 0644)
	fmt.Println("🔑 ECDSA key generated")
}

var log = logrus.New()

func main() {
	// parse some flags to set our nickname and the room to join
	flag.Var(&bootstrappers, "connect", "Connect to target bootstrap node. This can be any chat node on the network.")
	nickFlag := flag.String("nick", "", "Nickname to use in chat, generated if empty")
	roomFlag := flag.String("room", "main", "Name of chat room to join")
	listenHost := flag.String("host", "0.0.0.0", "The bootstrap node host listen address")
	port := flag.Int("port", 0, "The node's listening port. This is useful if using this node as a bootstrapper.")
	// anonymous := flag.Bool("anon", false, "Use an anonymous, ephemeral node identity. This option bypasses the use of your node's keypair.")
	useKey := flag.Bool("use-key", false, "Use an ECSDS keypair as this node's identifier. The keypair is generated if it does not exist in the app's local config directory.")
	info := flag.Bool("info", false, "Display node endpoint information before logging into the main chat room")
	flag.Parse()

	// Ensure config directory exists
	configPath := configdir.LocalConfig("chatp2p")
	er := configdir.MakePath(configPath) // Ensure it exists.
	if er != nil {
		panic(er)
	}

	// set up logging
	logfile := configdir.LocalConfig("chatp2p", "chatp2p.log")
	file, erro := os.OpenFile(logfile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if erro == nil {
		log.Out = file
	} else {
		log.Info("Failed to log to file, using default stderr")
	}

	fmt.Println(configPath)
	keyfile := configdir.LocalConfig("chatp2p", ".key")
	if _, err := os.Stat(keyfile); os.IsNotExist(err) {
		createKey()
	}

	ctx := context.Background()

	listenAddrs := libp2p.ListenAddrStrings(
		fmt.Sprintf("/ip4/%s/tcp/%d", *listenHost, *port),
	)

	// DHT Peer routing
	var dht *kaddht.IpfsDHT
	newDHT := func(h host.Host) (routing.PeerRouting, error) {
		var err error
		dht, err = kaddht.New(ctx, h)
		return dht, err
	}
	routing := libp2p.Routing(newDHT)

	var h host.Host
	var err error
	if *useKey {
		pk := getKey()
		h, err = libp2p.New(ctx, listenAddrs, routing,
			libp2p.Identity(pk))
		LogInfo("🔐 Generated identity from key:", h.ID().Pretty())
	} else {
		h, err = libp2p.New(ctx, listenAddrs, routing)
	}
	if err != nil {
		log.Error(err)
		panic(err)
	}

	// create a new PubSub service using the GossipSub router
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		log.Error(err)
		panic(err)
	}

	// setup local mDNS discovery
	err = setupDiscovery(ctx, h)
	if err != nil {
		log.Error(err)
		panic(err)
	}

	// use the nickname from the cli flag, or a default if blank
	nick := *nickFlag
	if len(nick) == 0 {
		nick = defaultNick(h.ID())
	}

	// join the room from the cli flag, or the flag default
	room := *roomFlag

	// join the chat room
	cr, err := JoinChatRoom(ctx, ps, h.ID(), nick, room)
	if err != nil {
		log.Error(err)
		panic(err)
	}

	if len(bootstrappers) == 0 {
		LogInfo("🔔 No bootstrappers defined for this node.")
	}

	for _, s := range bootstrappers {
		//fmt.Println("Connecting to bootstrap node: ", s)
		targetAddr, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			log.Error(err)
			panic(err)
		}

		targetInfo, err := peer.AddrInfoFromP2pAddr(targetAddr)
		if err != nil {
			log.Error(err)
			panic(err)
		}

		err = h.Connect(ctx, *targetInfo)
		if err != nil {
			log.Error(err)
			panic(err)
		}

		LogInfo("📞 Connected to bootstrap peer:", targetInfo.ID)
	}

	if *info {
		fmt.Print("👢 Available endpoints: \n")
		for _, addr := range h.Addrs() {
			fmt.Printf("	%s/p2p/%s\n", addr, h.ID().Pretty())
			log.Info("	%s/p2p/%s\n", addr, h.ID().Pretty())
		}
		fmt.Println("Press any key to continue...")
		fmt.Scanln() // wait for Enter Key
	}

	// draw the UI
	ui := NewChatUI(cr)
	if err = ui.Run(); err != nil {
		printErr("error running text UI: %s", err)
		log.Error("error running text UI: %s", err)
	}
}

// LogInfo logs to console and logger
func LogInfo(m string, args ...interface{}) {
	fmt.Println(m, args)
	log.Info(m, args)
}

// printErr is like fmt.Printf, but writes to stderr.
func printErr(m string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, m, args...)
}

// defaultNick generates a nickname based on the $USER environment variable and
// the last 8 chars of a peer ID.
func defaultNick(p peer.ID) string {
	return fmt.Sprintf("%s-%s", os.Getenv("USER"), shortID(p))
}

// shortID returns the last 8 chars of a base58-encoded peer id.
func shortID(p peer.ID) string {
	pretty := p.Pretty()
	return pretty[len(pretty)-8:]
}

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	h host.Host
}

// HandlePeerFound connects to peers discovered via mDNS. Once they're connected,
// the PubSub system will automatically start interacting with them if they also
// support PubSub.
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	log.Info("discovered new peer %s\n", pi.ID.Pretty())
	err := n.h.Connect(context.Background(), pi)
	if err != nil {
		log.Error("error connecting to peer %s: %s\n", pi.ID.Pretty(), err)
	}
}

// setupDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
func setupDiscovery(ctx context.Context, h host.Host) error {
	// setup mDNS discovery to find local peers
	disc, err := discovery.NewMdnsService(ctx, h, DiscoveryInterval, DiscoveryServiceTag)
	if err != nil {
		return err
	}

	n := discoveryNotifee{h: h}
	disc.RegisterNotifee(&n)
	return nil
}