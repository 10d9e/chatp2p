# chatp2p ☕

Experimentation with libp2p gossipsub, dht, and peer discovery with a simple chat application. This is a fully decentralized chat app whereby users can connect and chat with other users without the need for a central server. It is entirely possible to stand up an adhoc chat network anywhere.

Out of box, this dapp can be used on a local network without explicitely connecting out to bootstrap nodes, using mdns on a local network. When connected to other external nodes, peer discovery automatically connects all visible nodes to each other via a gossip protocol.

## Building 
In the builds directory, run the `build_release.sh` script to build macos, windows and linux binaries (in `builds/release/`).

```
% cd builds && build_release.sh
% ls release 
chatp2p-darwin-amd64			chatp2p-linux-amd64			chatp2p-windows-amd64.exe
chatp2p-darwin-amd64.checksum		chatp2p-linux-amd64.checksum		chatp2p-windows-amd64.exe.checksum
```

## Help
`./chat-p2p -help`
```
% ./chat-p2p -help     
Usage of ./chat-p2p:
  -connect value
        Connect to target bootstrap node. This can be any chat node on the network.
  -host string
        The bootstrap node host listen address (default "0.0.0.0")
  -info
        Display node endpoint information before logging into the main chat room
  -nick string
        Nickname to use in chat, generated if empty
  -port int
        The node's listening port. This is useful if using this node as a bootstrapper.
  -room string
        Name of chat room to join (default "main")
  -use-key
        Use an ECSDS keypair as this node's identifier. The keypair is generated if it does not exist in the app's local config directory.
```

## Bootstrap Nodes
Although not explicitely required, any distributed decentralized network works best with some static seed nodes to get things wired up. A static port and ECDSA key identifier can be used to defined a more consistent address for other users to connect to, if you are planning to run as a bootstrap node. 

To use a defined port and locally generated key as your node's identity:

`./chat-p2p -use-key -port 42 -daemon`
```
🔐 Generated identity from key: [QmSPSTKS6BjAnVhiWRzdcRw9v94paU97A4MapFHrUAwKxo]
🔔 No bootstrappers defined for this node. []
👢 Available endpoints: 
        /ip4/127.0.0.1/tcp/42/p2p/QmSPSTKS6BjAnVhiWRzdcRw9v94paU97A4MapFHrUAwKxo
        /ip4/192.168.2.11/tcp/42/p2p/QmSPSTKS6BjAnVhiWRzdcRw9v94paU97A4MapFHrUAwKxo
```
This will allow you to inform other users that they can connect to your node using the option `-connect /ip4/<public-ip>/tcp/42/p2p/QmSPSTKS6BjAnVhiWRzdcRw9v94paU97A4MapFHrUAwKxo`. Having other nodes use the `Available endpoints` will initiate peer discovery with all known nodes on each side, propogating across the entire network.

### Configuring the Boostrap Daemon on macos/linux/*nix

To run the bootstrap node as a background process:

1. Create/edit a script `vi bootstrap_chatp2p.sh`
2. Add the following contents:
``` bash
#!/bin/bash

./chatp2p-linux -daemon -port 1984 -use-key > chatp2p-test.log 2>&1 &
```
3. Update the execute permissions: `chmod +x bootstrap_chatp2p.sh`
4. Run it: `./bootstrap_chatp2p.sh`. This will start up the node as a background process.
5. To check that it is still up and running: `ps aux | grep chatp2p`
6. To check the logs: `cat ~/.config/chatp2p/chatp2p.log`

## Connecting to bootstrap nodes
If we want to connect clients to our bootstrap node and another known node, we can issue the following command:
`./chat-p2p -connect /ip4/35.224.203.143/tcp/4001/p2p/QmYmW8X9y16LyxpB82ossgw9h18xBu7dHR5wWHcB1hGA1p -connect /ip4/127.0.0.1/tcp/42/p2p/QmSKe3vVEA7wXGfCnpWsRnnWjY79giiQKKnnSsxhbK47XP`

results in something like:

```
📞 Connected to bootstrap peer: QmYmW8X9y16LyxpB82ossgw9h18xBu7dHR5wWHcB1hGA1p
📞 Connected to bootstrap peer: QmSKe3vVEA7wXGfCnpWsRnnWjY79giiQKKnnSsxhbK47XP
👢 Available endpoints: 
        /ip4/127.0.0.1/tcp/62929/p2p/QmTJH13KMsiBHdHrLx21Fbxbhdx28BNo4NDehXFDu3BWXz
        /ip4/192.168.2.11/tcp/62929/p2p/QmTJH13KMsiBHdHrLx21Fbxbhdx28BNo4NDehXFDu3BWXz
        /ip4/127.0.0.1/tcp/62930/ws/p2p/QmTJH13KMsiBHdHrLx21Fbxbhdx28BNo4NDehXFDu3BWXz
        /ip4/192.168.2.11/tcp/62930/ws/p2p/QmTJH13KMsiBHdHrLx21Fbxbhdx28BNo4NDehXFDu3BWXz
```

## Use a Handle
To update your nickname handle use the option:
`./chat-p2p -nick 💊satoasti_notacrumbo💊`

## Join a specific room
To update your nickname handle use the option:
`./chat-p2p -room 0xbitcoin-chatroom`

