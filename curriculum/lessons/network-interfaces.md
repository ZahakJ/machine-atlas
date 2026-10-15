---
{
  "id": "network-interfaces",
  "title": "Where does this machine meet the network?",
  "subsystem": "networking",
  "kind": "core",
  "order": 1,
  "summary": "Network interfaces as the kernel sees them, physical and virtual: link types, MTU, operational state and carrier, hardware addresses, and the IP addresses on each, with their scopes and the private, link-local and global ranges.",
  "prerequisites": [],
  "uses": [
    "derived.env.kind",
    "derived.net.default_route_interface",
    "derived.net.default_route_mtu",
    "derived.net.default_route_type",
    "derived.net.global_address",
    "derived.net.global_ipv6",
    "derived.net.interface_count",
    "derived.net.physical_interfaces",
    "net.addresses.ipv4",
    "net.addresses.ipv6",
    "net.interfaces",
    "net.sysctl.ipv6.conf.all.disable_ipv6"
  ],
  "relevance": [
    {"when": "derived.env.kind", "is": "container", "note": "Atlas runs in a container, which has its own network namespace: the interfaces here are the container's, not the host's."},
    {"when": "derived.net.default_route_type", "is": "wireless", "note": "This machine reaches the network over Wi-Fi."}
  ],
  "next": ["packet-path"],
  "deeper": ["planned:namespaces-and-isolation", "planned:name-resolution"],
  "version_notes": [
    {"applies": "systemd ≥ 197", "text": "udev names interfaces after where the device sits (enp3s0: PCI bus 3, slot 0; wlp4s0 for wireless) instead of the order drivers loaded (eth0, wlan0), so names survive reboots and hardware changes. Many embedded and container setups keep the old names."},
    {"applies": "Linux ≥ 3.17", "text": "addr_assign_type in sysfs says where a MAC address came from: the hardware (0), randomly generated (1), copied from another device, as a bond's is (2), or set from user space (3)."},
    {"applies": "iproute2 ≥ 4.13", "text": "ip prints JSON with -json, which is how Machine Atlas reads IPv4 addresses; older ip and busybox's applet print only text."}
  ],
  "references": [
    {"title": "Linux kernel documentation: Operational States (operstate, carrier, dormant)", "url": "https://docs.kernel.org/networking/operstates.html", "kind": "kernel-doc"},
    {"title": "Linux kernel ABI documentation: /sys/class/net", "url": "https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-net", "kind": "kernel-doc"},
    {"title": "Linux kernel documentation: IP sysctl (disable_ipv6, use_tempaddr)", "url": "https://docs.kernel.org/networking/ip-sysctl.html", "kind": "kernel-doc"},
    {"title": "netdevice(7)", "url": "https://man7.org/linux/man-pages/man7/netdevice.7.html", "kind": "man-page"},
    {"title": "ip-address(8)", "url": "https://man7.org/linux/man-pages/man8/ip-address.8.html", "kind": "man-page"},
    {"title": "ip-link(8)", "url": "https://man7.org/linux/man-pages/man8/ip-link.8.html", "kind": "man-page"},
    {"title": "RFC 1918: Address Allocation for Private Internets", "url": "https://www.rfc-editor.org/rfc/rfc1918", "kind": "spec"},
    {"title": "RFC 4291: IP Version 6 Addressing Architecture", "url": "https://www.rfc-editor.org/rfc/rfc4291", "kind": "spec"},
    {"title": "RFC 4193: Unique Local IPv6 Unicast Addresses", "url": "https://www.rfc-editor.org/rfc/rfc4193", "kind": "spec"},
    {"title": "RFC 8981: Temporary Address Extensions for Stateless Address Autoconfiguration in IPv6", "url": "https://www.rfc-editor.org/rfc/rfc8981", "kind": "spec"},
    {"title": "RFC 6598: IANA-Reserved IPv4 Prefix for Shared Address Space (100.64.0.0/10)", "url": "https://www.rfc-editor.org/rfc/rfc6598", "kind": "spec"},
    {"title": "systemd: Predictable Network Interface Names", "url": "https://systemd.io/PREDICTABLE_INTERFACE_NAMES/", "kind": "other"}
  ]
}
---
## Question

:::when derived.net.physical_interfaces
The kernel on this machine lists {{derived:derived.net.interface_count}} network interfaces, yet only {{derived:derived.net.physical_interfaces}} of them are backed by a network card or a radio. The others carry packets too, but no cable or antenna is behind them. What is a network interface, if not a piece of hardware, and which of these does traffic to the internet actually leave through?
:::else
Machine Atlas sees {{derived:derived.net.interface_count}} network interfaces here, and none of them is backed by hardware: no Ethernet card, no Wi-Fi radio. Packets still reach the internet from this environment. What is a network interface, if not a piece of hardware, and where do these packets go next?
:::unknown
Every Linux machine has several network interfaces, and on most of them only some are backed by hardware: the rest carry packets with no cable or antenna behind them. Here the interface list could not be read ({{derived:derived.net.interface_count}}). What is a network interface, if not a piece of hardware, and how can a program learn which one its traffic leaves through?
:::end

## Why it exists

The network stack has one job for every packet: hand it to something that can carry it towards its destination. That something can be very different things. An Ethernet controller drives a cable, a Wi-Fi chip a radio. The loopback device carries packets from this machine to itself. A bridge joins several links into one switch in software, a bond spreads traffic over several cables, a veth pair carries packets between two network namespaces, and a tunnel hands them to a VPN program that encrypts them and sends them out again over another interface.

If routing, queueing, filtering and counting had to know about each of these, every new kind of device would mean rewriting the stack. So Linux gives them all one shape, the *network device* (`struct net_device`): something with a name, a type, a maximum packet size, a transmit function, queues and counters. The stack decides which device a packet goes to; the device decides what that means.

## How it works

### A device, a type and a state

Each interface appears in `/sys/class/net/<name>/`. Its `type` is a hardware type number from the ARP protocol's registry (1 for Ethernet framing, 772 for loopback, 65534 for devices without a link-layer header such as WireGuard and tun). Ethernet framing alone says little, because Wi-Fi, bridges, bonds and veths all use it. The presence of `wireless/`, `bridge/`, `bonding/` or a `device` link to a bus device tells them apart, and the `device/driver` link names the driver (e1000e, iwlwifi, mlx5_core, virtio_net).

The **MTU** is the largest IP packet the device sends in one frame: 1500 bytes on Ethernet since the 1980s, 65536 on loopback, often 9000 (*jumbo frames*) inside data centres, and less on tunnels, which must fit their own header inside the MTU of the link below.

Two states describe whether it can carry traffic. The administrative state is what someone asked for (`ip link set up`). The operational state, `operstate`, is what the kernel concluded, following RFC 2863: `up` when it can pass packets, `down` when it cannot, `dormant` while it waits for something such as Wi-Fi authentication, `lowerlayerdown` when a device it sits on is down, and `unknown` when the driver does not say, which is normal for loopback and tunnels. `carrier` reports the physical layer: 1 while a cable is plugged into something live or the radio is associated. An interface switched off administratively answers neither `carrier`, `speed` nor `duplex`: the files return an error.

### Hardware addresses

Ethernet-framed devices have a 48-bit MAC address. The first three bytes of a manufacturer's address identify the manufacturer, and bit 1 of the first byte says whether the address is *universally* administered (assigned by a manufacturer, unique in the world) or *locally* administered (made up by software). Virtual devices, containers' veths and Wi-Fi interfaces that randomise their address to avoid being tracked between networks all use local addresses. A bond takes its first member's address and gives it to the others. The MAC matters only on the local link: routers replace it at every hop.

### IP addresses belong to interfaces

An IP address is configured on an interface, with a prefix length that says which neighbours are directly reachable through it: `192.168.1.23/24` puts every address from `192.168.1.0` to `192.168.1.255` on the same link. Configuring it creates a route for that prefix automatically. The address still belongs to the whole machine: by default Linux answers for any of its addresses on any interface (the *weak host* model).

Each address has a **scope**: `host` for loopback addresses, which never leave the machine; `link` for link-local addresses (IPv4 `169.254.0.0/16`, IPv6 `fe80::/10`), valid only on the link they were configured on; `global` for everything else. IPv6 gives every interface a link-local address automatically, because neighbour discovery and router advertisements run over it.

A global scope does not make an address public. The ranges fall into classes:

- **private** IPv4 (RFC 1918: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) and **unique local** IPv6 (`fc00::/7`, in practice `fd00::/8`) are for networks that are not routed on the internet; reaching the internet from them needs address translation (NAT) somewhere on the way;
- **cgnat** (`100.64.0.0/10`) is the shared space carriers use for their own layer of NAT, and some VPNs borrow;
- **global** addresses are routed on the internet.

IPv6 hosts usually build their global addresses themselves from the router's advertised prefix (stateless autoconfiguration). Because a stable address would let every server recognise the machine, they also create **temporary** addresses with random interface identifiers, used for outgoing connections and replaced every day or so (RFC 8981).

Machine Atlas reads IPv6 addresses from `/proc/net/if_inet6` and IPv4 addresses with `ip -json`, because no file lists IPv4 addresses by interface. Exported snapshots never contain the addresses themselves: every redaction policy replaces them, and only the class, scope and prefix length remain.

## On this machine

:::table net.interfaces columns=name,type,operstate,carrier,mtu,speed_mbps,driver,master,mac_scope,mac_origin

:::evidence Interfaces and the default route
- net.interfaces | Interfaces in Atlas's network namespace
- derived.net.physical_interfaces | Backed by an Ethernet or Wi-Fi device
- derived.net.default_route_interface | Where traffic to the internet leaves
- derived.net.default_route_type
- derived.net.default_route_mtu
- derived.env.kind
:::end

:::when derived.env.kind is container
Atlas runs in a container, and a container gets its own network namespace: a private copy of the interface list, addresses, routes and sockets. The interfaces above are the container's own, typically `lo` and one end of a veth pair whose other end sits on a bridge in the host's namespace, where the host's real NICs are. Nothing in this namespace shows them.
:::else
:::when derived.net.default_route_type is wireless
Traffic to the internet leaves through {{derived:derived.net.default_route_interface}}, a wireless interface with an MTU of {{derived:derived.net.default_route_mtu}} bytes. Wi-Fi drivers report no link speed, because the rate changes from moment to moment with signal quality, so `speed_mbps` stays empty for it.
:::else
Traffic to the internet leaves through {{derived:derived.net.default_route_interface}}, an interface of type {{derived:derived.net.default_route_type}} with an MTU of {{derived:derived.net.default_route_mtu}} bytes. The other interfaces carry traffic for this machine itself (loopback), for its containers and VMs (bridges, veths), or for a VPN (tunnels).
:::unknown
Which interface carries traffic to the internet could not be determined here ({{derived:derived.net.default_route_interface}}): the routing table or the interface list is unreadable. On most machines it is the one wired or wireless interface that is up.
:::end
:::unknown
Machine Atlas could not tell whether it runs in a container here ({{derived:derived.env.kind}}). If it does, the interfaces above are the container's own and not the host's.
:::end

### Addresses

:::table net.addresses.ipv4 columns=interface,address,prefix_len,scope,class,dynamic

:::table net.addresses.ipv6 columns=interface,address,prefix_len,scope,class,dynamic,temporary

:::when derived.net.global_address
At least one address here is global: traffic sent from it reaches the internet without its source address being translated, and can be reached from the internet as far as firewalls allow. Traffic from any private address in the same tables is still translated on its way out.
:::else
None of this machine's addresses is global: they are private, unique local, link-local or loopback. To reach the internet, its packets pass through a router or host that translates their source address to one of its own (NAT), and connections from the internet cannot reach it unless that router forwards a port.
:::unknown
Whether this machine has a global address could not be determined ({{derived:derived.net.global_address}}): one of the address lists is unavailable here. The tables above show what could be read.
:::end

:::when net.sysctl.ipv6.conf.all.disable_ipv6 is 1
IPv6 is disabled on every interface here (`disable_ipv6` is {{obs:net.sysctl.ipv6.conf.all.disable_ipv6}}), so there are no IPv6 addresses at all, not even link-local ones. Container runtimes do this when the container's network has no IPv6.
:::else
:::when derived.net.global_ipv6
IPv6 is enabled and this machine has a global IPv6 address. Look for rows marked `temporary` among them: those are the privacy addresses used for outgoing connections.
:::else
IPv6 is enabled, but no interface has a global IPv6 address, only link-local ones (and `::1` on loopback). The local network does not advertise an IPv6 prefix, so this machine reaches the internet over IPv4 only.
:::unknown
Whether this machine has a global IPv6 address could not be determined ({{derived:derived.net.global_ipv6}}).
:::end
:::unknown
Whether IPv6 is enabled could not be read here ({{obs:net.sysctl.ipv6.conf.all.disable_ipv6}}); the kernel may have been built or booted without it.
:::end

## Predict

:::predict
The Ethernet cable of a desktop with a working wired connection is unplugged. What happens to its interface in `/sys/class/net` in the first second?

- The interface disappears until the cable is plugged in again.
- The interface stays; `carrier` becomes 0 and `operstate` becomes `down`, while its IP addresses stay configured.
- The interface stays and stays `up`; nothing changes until a packet fails to arrive.
:::reveal
The second. The interface is the kernel's handle for the controller, which is still there, so it stays. The PHY reports loss of link, the driver calls `netif_carrier_off()`, `carrier` reads 0 and `operstate` changes to `down`. The kernel does not remove addresses: the routes through the interface stay too, marked `linkdown`, so `ip route` still prints them. A network manager listening for the event may then remove a DHCP address or switch the default route to Wi-Fi, but that is user space reacting, not the kernel.

The first answer is what happens when a USB adapter is unplugged: the device itself goes, and with it the interface. The third describes a link without carrier detection, such as some virtual and tunnel devices, whose `operstate` stays `unknown` because nothing tells the kernel.
:::end

## Try it

```sh
ip -brief link         # name, operstate, MAC
ip -brief address      # name, operstate, addresses with prefix lengths
cat /sys/class/net/*/operstate
cat /sys/class/net/*/mtu
```

`ip -brief link` shows the same state as `operstate`; the flags in angle brackets of `ip link` (`UP`, `LOWER_UP`, `NO-CARRIER`) show the administrative state and the carrier separately. On a laptop, switching Wi-Fi off and on changes the wireless interface's `operstate` to `down` and back through `dormant` while it authenticates. `ethtool <interface>` prints the speed and duplex the driver negotiated, without root.

## Limits

:::misconception A network interface is a network card
Most interfaces on a typical machine have no hardware behind them: loopback, bridges, veth pairs, tunnels, bonds. And one card can carry several interfaces, such as VLANs or the virtual functions of an SR-IOV NIC.
:::end

:::misconception The MAC address identifies the machine on the internet
It never leaves the local link: every router rewrites the source MAC of each frame to its own. Many devices also replace it with a random, locally administered one per network.
:::end

:::misconception A private address means no internet access
It means the address is not routed on the internet. Most home and office machines have only private addresses and reach the internet through NAT; what they lose is being reachable from the internet without help.
:::end

:::cannot
Which networks are actually reachable: an interface can be `up` with an address while the router behind it is down. Only sending packets tests that.

What is on the other side of a virtual interface. The peer of a container's veth lives in another network namespace, and a tunnel's real traffic leaves through another interface after encryption; neither is visible from here.

Wi-Fi network names, signal strength and the negotiated Wi-Fi rate: Machine Atlas does not collect them, and the first identifies places a machine has been.

Addresses on interfaces of other network namespaces, and addresses that firewall rules translate this machine's traffic to.
:::end

## Go deeper

Next, [the path of a packet](lesson:packet-path) follows a packet from a program's `send()` through the routing decision and the queues to the interface, and back up from the NIC. In Explore, [network interfaces](component:network-interfaces) lists this machine's interfaces with the bridges and bonds they belong to, and [network interface controllers](component:nic) describes the hardware below them. Namespaces, which give each container its own interface list, are a planned lesson in the processes card; so is name resolution, how a name such as `example.org` becomes one of these addresses.
