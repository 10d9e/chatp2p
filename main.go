package main

import (
	"bufio"
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
	connmgr "github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	cr "github.com/libp2p/go-libp2p-core/routing"
	crypto "github.com/libp2p/go-libp2p-crypto"
	discovery2 "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	libp2pquic "github.com/libp2p/go-libp2p-quic-transport"
	secio "github.com/libp2p/go-libp2p-secio"
	libp2ptls "github.com/libp2p/go-libp2p-tls"
	"github.com/libp2p/go-libp2p/p2p/discovery"
	"github.com/multiformats/go-multiaddr"

	"github.com/sirupsen/logrus"

	"github.com/libp2p/go-libp2p-core/host"
	pubsub "github.com/libp2p/go-libp2p-pubsub"

	"github.com/kirsle/configdir"
	"github.com/matryer/try"
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

const bootstrapPeer = "/ip4/35.224.203.143/tcp/4001/p2p/QmeRw9ZbkupTq89mrsXFX87pxzYpXR9Bmems25LPKvPbwQ"

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

	configSetup()

	ctx := context.Background()

	listenAddrs := libp2p.ListenAddrStrings(
		fmt.Sprintf("/ip4/%s/tcp/%d", *listenHost, *port),
		fmt.Sprintf("/ip4/%s/udp/%d/quic", *listenHost, *port),
	)

	var err error
	// DHT Peer routing
	var idht *dht.IpfsDHT
	routing := libp2p.Routing(func(h host.Host) (cr.PeerRouting, error) {
		targetAddr, err := multiaddr.NewMultiaddr(bootstrapPeer)
		//dht.DefaultBootstrapPeers = append(dht.DefaultBootstrapPeers, targetAddr)
		addrInfo, _ := peer.AddrInfoFromP2pAddr(targetAddr)

		/*
			dht.DefaultBootstrapPeers = nil
			ma, err := multiaddr.NewMultiaddr(bootstrapPeer)
			if err != nil {
				panic(err)
			}
			dht.DefaultBootstrapPeers = append(dht.DefaultBootstrapPeers, ma)
		*/

		dht.DefaultBootstrapPeers = nil

		// idht, err = dht.New(ctx, h, dht.RoutingTableRefreshPeriod(10*time.Second), dht.Mode(dht.ModeServer))

		idht, err = dht.New(ctx, h,
			dht.Mode(dht.ModeServer),
			dht.ProtocolPrefix("/chatp2p/kad/1.0.0"),
			dht.BootstrapPeers(*addrInfo),
			// dht.RoutingTableRefreshPeriod(10*time.Second),
		)

		idht.RoutingTable().PeerAdded = func(p peer.ID) {
			fmt.Println("+++ idht.PeerAdded", p)
		}
		idht.RoutingTable().PeerRemoved = func(p peer.ID) {
			fmt.Println("--- idht.PeerRemoved", p)
		}

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

	/*
		not := cm.Notifee()

		not.Disconnected(func(n network.Network, c network.Conn) {
			fmt.Println("*** Disconnect callback")
		})
	*/

	var h host.Host
	if *useKey {
		pk := getKey()
		h, err = libp2p.New(ctx,
			listenAddrs,
			// support TLS connections
			libp2p.Security(libp2ptls.ID, libp2ptls.New),
			// support secio connections
			libp2p.Security(secio.ID, secio.New),
			// support QUIC - experimental
			libp2p.Transport(libp2pquic.NewTransport),
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
			listenAddrs,
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

	// attempt to connect to boostrappers
	//connectBootstrapPeers(ctx, h, bootstrappers)

	err = setupDHTDiscovery(ctx, h, *roomFlag)
	if err != nil {
		log.Error(err)
		panic(err)
	}

	// This connects to public bootstrappers
	/*
		for _, addr := range dht.DefaultBootstrapPeers {

			LogInfo("🔔 Calling DHT bootstrap peer:", addr)

			targetInfo, err := peer.AddrInfoFromP2pAddr(addr)
			if err != nil {
				log.Error(err)
				//return
			}

			err = h.Connect(ctx, *targetInfo)
			if err != nil {
				log.Error(err)
				//return
			} else {
				LogInfo("📞 Connected to bootstrap peer:", targetInfo.ID)
			}

		}
	*/

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

	h.Network().Notify(&network.NotifyBundle{
		ListenF: func(n network.Network, m multiaddr.Multiaddr) {
			fmt.Println("*** ListenCloseF")
		},
		ListenCloseF: func(n network.Network, m multiaddr.Multiaddr) {
			fmt.Println("*** ListenCloseF")
		},
		ConnectedF: func(n network.Network, c network.Conn) {
			fmt.Println("*** ConnectedF", cr.ListPeers(), c.ID())
		},
		DisconnectedF: func(n network.Network, c network.Conn) {

			if strings.HasPrefix(string(c.ID()), "QmeRw9Zbku") {
				fmt.Println("*** DisconnectedF", cr.ListPeers(), c.ID())
				//tryReconnectBootstrapper(ctx, h)
			}

			/*
				pid, _ := peer.IDFromString("QmeRw9ZbkupTq89mrsXFX87pxzYpXR9Bmems25LPKvPbwQ")
				if !containsPeer(cr.ListPeers(), pid) {
					fmt.Println("*** DisconnectedF", cr.ListPeers(), c.ID())
				}
			*/
		},
		OpenedStreamF: func(network.Network, network.Stream) {
		},
		ClosedStreamF: func(network.Network, network.Stream) {
		},
	})

	if *info {
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

func tryReconnectBootstrapper(ctx context.Context, h host.Host) {
	err := try.Do(func(attempt int) (bool, error) {
		var err error
		err = doConnectBootstrap(ctx, h, bootstrapPeer)
		if err != nil {
			time.Sleep(10 * time.Second) // wait a bit
		}
		return attempt < 20, err
	})
	if err != nil {
		log.Fatalln("error:", err)
	}

}

func doConnectBootstrap(ctx context.Context, h host.Host, s string) error {
	LogInfo("🔔 Calling bootstrap peer:", s)
	targetAddr, err := multiaddr.NewMultiaddr(s)
	if err != nil {
		log.Error(err)
		return err
	}

	targetInfo, err := peer.AddrInfoFromP2pAddr(targetAddr)
	if err != nil {
		log.Error(err)
		return err
	}

	err = h.Connect(ctx, *targetInfo)
	if err != nil {
		log.Error(err)
		return err
	}

	LogInfo("📞 Connected to bootstrap peer:", targetInfo.ID)
	return nil
}

func containsPeer(a []peer.ID, x peer.ID) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

func configSetup() {
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
	bootstrapFile := configdir.LocalConfig("chatp2p", "bootstrappers")
	if _, err := os.Stat(bootstrapFile); os.IsNotExist(err) {
		d1 := []byte(bootstrapPeer + "\n")
		err := ioutil.WriteFile(bootstrapFile, d1, 0644)
		if err != nil {
			log.Error(err)
			panic(err)
		}
	}
}

func connectBootstrapPeers(ctx context.Context, h host.Host, bootstrappers []string) {

	bootstrapFile := configdir.LocalConfig("chatp2p", "bootstrappers")
	file, err := os.Open(bootstrapFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		bootstrappers = append(bootstrappers, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	if len(bootstrappers) == 0 {
		LogInfo("🔔 No bootstrappers defined for this node.")
	}

	for _, s := range bootstrappers {
		LogInfo("🔔 Calling bootstrap peer:", s)
		targetAddr, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			log.Error(err)
			return
		}

		targetInfo, err := peer.AddrInfoFromP2pAddr(targetAddr)
		if err != nil {
			log.Error(err)
			return
		}

		err = h.Connect(ctx, *targetInfo)
		if err != nil {
			log.Error(err)
			return
		}

		LogInfo("📞 Connected to bootstrap peer:", targetInfo.ID)
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

func setupDHTDiscovery(ctx context.Context, host host.Host, roomFlag string) error {

	// Start a DHT, for use in peer discovery. We can't just make a new DHT
	// client because we want each peer to maintain its own local copy of the
	// DHT, so that the bootstrapping node of the DHT can go down without
	// inhibiting future peer discovery.

	kademliaDHT, err := dht.New(ctx, host, dht.Mode(dht.ModeServer), dht.ProtocolPrefix("chatp2p/1.0.0"))
	if err != nil {
		panic(err)
	}

	kademliaDHT.RoutingTable().PeerAdded = func(p peer.ID) {
		fmt.Println("+++ setupDHTDiscovery.PeerAdded", p)
	}
	kademliaDHT.RoutingTable().PeerRemoved = func(p peer.ID) {
		fmt.Println("--- setupDHTDiscovery.PeerRemoved", p)
	}

	// Bootstrap the DHT. In the default configuration, this spawns a Background
	// thread that will refresh the peer table every five minutes.
	log.Debug("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		panic(err)
	}

	// Let's connect to the bootstrap nodes first. They will tell us about the
	// other nodes in the network.
	/*
		var wg sync.WaitGroup
		for _, peerAddr := range config.BootstrapPeers {
			peerinfo, _ := peer.AddrInfoFromP2pAddr(peerAddr)
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := host.Connect(ctx, *peerinfo); err != nil {
					log.Warning(err)
				} else {
					log.Info("Connection established with bootstrap node:", *peerinfo)
				}
			}()
		}
		wg.Wait()
	*/

	// We use a rendezvous point "meet me here" to announce our location.
	// This is like telling your friends to meet you at the Eiffel Tower.
	log.Info("Announcing ourselves...")
	routingDiscovery := discovery2.NewRoutingDiscovery(kademliaDHT)
	discovery2.Advertise(ctx, routingDiscovery, roomFlag)
	log.Debug("Successfully announced!")

	// Now, look for others who have announced
	// This is like your friend telling you the location to meet you.
	log.Debug("Searching for other peers...")
	peerChan, err := routingDiscovery.FindPeers(ctx, roomFlag)
	if err != nil {
		panic(err)
	}

	for peer := range peerChan {
		if peer.ID == host.ID() {
			continue
		}
		log.Info("Found peer:", peer)

		log.Info("Connecting to:", peer)

		/*
			if err := host.Connect(ctx, *peer.Addrs); err != nil {
				log.Warning(err)
			} else {
				log.Info("Connection established with bootstrap node:", *peerinfo)
			}
		*/

		/*
			stream, err := host.NewStream(ctx, peer.ID, protocol.ID(config.ProtocolID))

			if err != nil {
				log.Warning("Connection failed:", err)
				continue
			} else {
				rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

				go writeData(rw)
				go readData(rw)
			}
		*/

		log.Info("Connected to:", peer)
	}

	return nil
}
