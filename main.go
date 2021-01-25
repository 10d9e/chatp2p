package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/libp2p/go-libp2p"
	connmgr "github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	cr "github.com/libp2p/go-libp2p-core/routing"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	secio "github.com/libp2p/go-libp2p-secio"
	libp2ptls "github.com/libp2p/go-libp2p-tls"
	"github.com/libp2p/go-libp2p/p2p/discovery"
	"github.com/sirupsen/logrus"
)

// DiscoveryInterval is how often we re-publish our mDNS records.
const DiscoveryInterval = time.Hour

// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
const DiscoveryServiceTag = "chatp2p"

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

var log = logrus.New()

func main() {
	// parse some flags to set our nickname and the room to join
	flag.Var(&bootstrappers, "connect", "Connect to target bootstrap node. This can be any chat node on the network.")
	nickFlag := flag.String("nick", "", "Nickname to use in chat, generated if empty")
	roomFlag := flag.String("room", "main", "Name of chat room to join")
	listenHost := flag.String("host", "0.0.0.0", "The bootstrap node host listen address")
	port := flag.Int("port", 0, "The node's listening port. This is useful if using this node as a bootstrapper.")
	useKey := flag.Bool("use-key", false, "Use an ECSDS keypair as this node's identifier. The keypair is generated if it does not exist in the app's local config directory.")
	info := flag.Bool("info", false, "Display node endpoint information before logging into the main chat room")
	daemon := flag.Bool("daemon", false, "Run as a boostrap daemon only")
	flag.Parse()

	conf := ConfigSetup()

	ctx := context.Background()

	var err error
	// DHT Peer routing
	var idht *dht.IpfsDHT
	routing := libp2p.Routing(func(h host.Host) (cr.PeerRouting, error) {
		dht.DefaultBootstrapPeers = nil
		bootstrapPeers, err := CollectBootstrapAddrInfos(ctx)
		idht, err = dht.New(ctx, h,
			dht.Mode(dht.ModeServer),
			dht.ProtocolPrefix("/chatp2p/kad/1.0.0"),
			dht.BootstrapPeers(bootstrapPeers...),
		)

		fmt.Println("Bootstrapping the DHT")
		if err = idht.Bootstrap(ctx); err != nil {
			panic(err)
		}
		return idht, err
	})

	cm := connmgr.NewConnManager(
		100,         // Lowwater
		400,         // HighWater,
		time.Minute, // GracePeriod
	)

	psk, _ := ClusterSecret()

	var h host.Host
	if *useKey {
		pk := GetKey()
		h, err = libp2p.New(ctx,
			// use a private network
			libp2p.PrivateNetwork(psk),
			// listen addresses
			libp2p.ListenAddrStrings(
				fmt.Sprintf("/ip4/%s/tcp/%d", *listenHost, *port),
			),
			// support TLS connections
			libp2p.Security(libp2ptls.ID, libp2ptls.New),
			// support secio connections
			libp2p.Security(secio.ID, secio.New),
			// support any other default transports (TCP)
			libp2p.DefaultTransports,
			// Let this host use the DHT to find other hosts
			routing,
			// Connection Manager
			libp2p.ConnectionManager(cm),
			// Attempt to open ports using uPNP for NATed hosts.
			libp2p.NATPortMap(),
			// Let this host use relays and advertise itself on relays if
			// it finds it is behind NAT. Use libp2p.Relay(options...) to
			// enable active relays and more.
			libp2p.EnableAutoRelay(),
			// Use the defined identity
			libp2p.Identity(pk),
		)
		LogInfo("🔐 Using identity from key:", h.ID().Pretty())
	} else {
		h, err = libp2p.New(ctx,
			// use a private network
			libp2p.PrivateNetwork(psk),
			// listen addressesß
			libp2p.ListenAddrStrings(
				fmt.Sprintf("/ip4/%s/tcp/%d", *listenHost, *port),
			),
			// support TLS connections
			libp2p.Security(libp2ptls.ID, libp2ptls.New),
			// support secio connections
			libp2p.Security(secio.ID, secio.New),
			// support any other default transports (TCP)
			libp2p.DefaultTransports,
			// Let this host use the DHT to find other hosts
			routing,
			// Connection Manager
			libp2p.ConnectionManager(cm),
			// Attempt to open ports using uPNP for NATed hosts.
			libp2p.NATPortMap(),
			// Let this host use relays and advertise itself on relays if
			// it finds it is behind NAT. Use libp2p.Relay(options...) to
			// enable active relays and more.
			libp2p.EnableAutoRelay(),
		)
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
	err = setupMdnsDiscovery(ctx, h)
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

	if *info {
		fmt.Println("🔖  Network id:", conf.ClusterKey)
		fmt.Print("👢 Available endpoints: \n")
		for _, addr := range h.Addrs() {
			fmt.Printf("	%s/p2p/%s\n", addr, h.ID().Pretty())
			log.Info("	%s/p2p/%s\n", addr, h.ID().Pretty())
		}
		fmt.Println("Press any key to continue...")
		fmt.Scanln() // wait for Enter Key
	}

	if *daemon {
		select {}
	} else {
		// draw the UI
		ui := NewChatUI(cr)
		if err = ui.Run(); err != nil {
			printErr("error running text UI: %s", err)
			log.Error("error running text UI: %s", err)
		}
	}
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
func setupMdnsDiscovery(ctx context.Context, h host.Host) error {
	// setup mDNS discovery to find local peers
	disc, err := discovery.NewMdnsService(ctx, h, DiscoveryInterval, DiscoveryServiceTag)
	if err != nil {
		return err
	}

	n := discoveryNotifee{h: h}
	disc.RegisterNotifee(&n)
	return nil
}
