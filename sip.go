package main

import (
    "fmt"
    "log"
    "net"
    "time"
    "github.com/cloudwebrtc/go-sip-ua/pkg/ua"
    "github.com/gordonklaus/portaudio"
    "github.com/pion/rtp"
    "github.com/pion/rtp/codecs/g711"
    "github.com/pion/opus"
    "github.com/pion/stun"
    "github.com/pion/turn/v2"
)

func main() {
    // Initialize PortAudio
    if err := portaudio.Initialize(); err != nil {
        log.Fatalf("Failed to initialize PortAudio: %v", err)
    }
    defer portaudio.Terminate()

    // Create a new SIP User Agent (UA)
    ua := ua.NewUA(&ua.UAConfig{
        UserAgent: "GoIPPhone/1.0",
    })

    // Register with the SIP server
    registerURI := "sip:example.com"
    username := "alice"
    password := "password"
    err := ua.Register(registerURI, username, password)
    if err != nil {
        log.Fatalf("Failed to register: %v", err)
    }
    fmt.Println("Registered successfully")

    // Handle incoming calls
    ua.OnInvite(func(session *ua.Session) {
        fmt.Println("Incoming call from:", session.RemoteURI)

        // Extract SDP from the INVITE request
        sdpOffer := session.RemoteSDP()
        fmt.Println("Received SDP Offer:", sdpOffer)

        // Perform NAT traversal (STUN with TURN fallback)
        publicIP, publicPort, relayIP, relayPort, err := performNATTraversal(nil)
        if err != nil {
            log.Fatalf("Failed to perform NAT traversal: %v", err)
        }
        fmt.Printf("Public IP and port: %s:%d\n", publicIP, publicPort)
        if relayIP != "" {
            fmt.Printf("TURN relay IP and port: %s:%d\n", relayIP, relayPort)
        }

        // Generate an SDP answer with the discovered addresses
        sdpAnswer := generateSDPAnswer(publicIP, publicPort, relayIP, relayPort)
        session.AcceptWithSDP(sdpAnswer)
        fmt.Println("Call answered with SDP:", sdpAnswer)

        // Handle RTP communication in a separate function
        go handleRTPCommunication(session, publicIP, publicPort, relayIP, relayPort)
    })

    // Make an outgoing call
    callee := "sip:bob@example.com"
    session, err := ua.Invite(callee, registerURI)
    if err != nil {
        log.Fatalf("Failed to initiate call: %v", err)
    }

    // Handle session events
    go func() {
        for event := range session.Events() {
            switch event.Type {
            case ua.EventTypeConnected:
                fmt.Println("Call connected")
                // Perform NAT traversal (STUN with TURN fallback)
                publicIP, publicPort, relayIP, relayPort, err := performNATTraversal(nil)
                if err != nil {
                    log.Fatalf("Failed to perform NAT traversal: %v", err)
                }
                fmt.Printf("Public IP and port: %s:%d\n", publicIP, publicPort)
                if relayIP != "" {
                    fmt.Printf("TURN relay IP and port: %s:%d\n", relayIP, relayPort)
                }
                // Handle RTP communication in a separate function
                go handleRTPCommunication(session, publicIP, publicPort, relayIP, relayPort)
            case ua.EventTypeDisconnected:
                fmt.Println("Call disconnected")
            case ua.EventTypeError:
                fmt.Printf("Call error: %v\n", event.Error)
            }
        }
    }()

    // Wait for the session to end
    <-session.Done()
    fmt.Println("Call ended")
}

// performNATTraversal performs STUN discovery with TURN fallback
func performNATTraversal(localAddr *net.UDPAddr) (string, int, string, int, error) {
    // Try STUN first
    publicIP, publicPort, err := performSTUNWithKeepalive(localAddr)
    if err == nil {
        return publicIP, publicPort, "", 0, nil // STUN succeeded
    }
    log.Printf("STUN failed: %v", err)

    // Fall back to TURN
    relayIP, relayPort, err := performTURN(localAddr)
    if err != nil {
        return "", 0, "", 0, fmt.Errorf("TURN fallback failed: %v", err)
    }
    return "", 0, relayIP, relayPort, nil // TURN succeeded
}

// performSTUNWithKeepalive discovers the public IP and port using STUN and sends keepalives
func performSTUNWithKeepalive(localAddr *net.UDPAddr) (string, int, error) {
    // Create a STUN client
    conn, err := net.ListenUDP("udp", localAddr)
    if err != nil {
        return "", 0, fmt.Errorf("failed to create UDP connection: %v", err)
    }
    defer conn.Close()

    client, err := stun.NewClient(conn)
    if err != nil {
        return "", 0, fmt.Errorf("failed to create STUN client: %v", err)
    }
    defer client.Close()

    // Send a STUN request to discover the public IP and port
    var publicIP string
    var publicPort int
    if err := client.Do(stun.MustBuild(stun.TransactionID, stun.BindingRequest), func(res stun.Event) {
        if res.Error != nil {
            err = res.Error
            return
        }

        // Decode the STUN response
        var xorAddr stun.XORMappedAddress
        if err := xorAddr.GetFrom(res.Message); err != nil {
            err = fmt.Errorf("failed to decode STUN response: %v", err)
            return
        }

        publicIP = xorAddr.IP.String()
        publicPort = xorAddr.Port
    }); err != nil {
        return "", 0, fmt.Errorf("failed to perform STUN request: %v", err)
    }

    // Send STUN keepalives to maintain the NAT mapping
    go func() {
        ticker := time.NewTicker(30 * time.Second) // Send keepalives every 30 seconds
        defer ticker.Stop()

        for {
            select {
            case <-ticker.C:
                if err := client.Do(stun.MustBuild(stun.TransactionID, stun.BindingRequest), nil); err != nil {
                    log.Printf("Failed to send STUN keepalive: %v", err)
                }
            case <-time.After(2 * time.Minute): // Stop keepalives after 2 minutes
                return
            }
        }
    }()

    return publicIP, publicPort, nil
}

// performTURN discovers the relay IP and port using TURN
func performTURN(localAddr *net.UDPAddr) (string, int, error) {
    // TURN server configuration
    turnServer := "turn.example.com:3478"
    username := "username"
    password := "password"

    // Create a TURN client
    conn, err := net.ListenUDP("udp", localAddr)
    if err != nil {
        return "", 0, fmt.Errorf("failed to create UDP connection: %v", err)
    }
    defer conn.Close()

    client, err := turn.NewClient(&turn.ClientConfig{
        STUNServerAddr: turnServer,
        TURNServerAddr: turnServer,
        Username:       username,
        Password:       password,
        Conn:           conn,
    })
    if err != nil {
        return "", 0, fmt.Errorf("failed to create TURN client: %v", err)
    }
    defer client.Close()

    // Allocate a relay address
    relayAddr, err := client.Allocate()
    if err != nil {
        return "", 0, fmt.Errorf("failed to allocate relay address: %v", err)
    }

    return relayAddr.IP.String(), relayAddr.Port, nil
}

// generateSDPAnswer generates an SDP answer with the discovered addresses
func generateSDPAnswer(publicIP string, publicPort int, relayIP string, relayPort int) string {
    if relayIP != "" {
        // Use TURN relay address
        return fmt.Sprintf("v=0\r\n"+
            "o=- 0 0 IN IP4 %s\r\n"+
            "s=-\r\n"+
            "c=IN IP4 %s\r\n"+
            "t=0 0\r\n"+
            "m=audio %d RTP/AVP 0 96\r\n"+ // Use TURN relay port
            "a=rtpmap:96 opus/8000/1\r\n", // Opus codec
            relayIP, relayIP, relayPort)
    }
    // Use STUN public address
    return fmt.Sprintf("v=0\r\n"+
        "o=- 0 0 IN IP4 %s\r\n"+
        "s=-\r\n"+
        "c=IN IP4 %s\r\n"+
        "t=0 0\r\n"+
        "m=audio %d RTP/AVP 0 96\r\n"+ // Use STUN public port
        "a=rtpmap:96 opus/8000/1\r\n", // Opus codec
        publicIP, publicIP, publicPort)
}

// handleRTPCommunication handles sending and receiving RTP packets
func handleRTPCommunication(session *ua.Session, publicIP string, publicPort int, relayIP string, relayPort int) {
    var rtpConn *net.UDPConn
    var err error

    if relayIP != "" {
        // Use TURN relay address
        rtpConn, err = net.DialUDP("udp", nil, &net.UDPAddr{
            IP:   net.ParseIP(relayIP),
            Port: relayPort,
        })
    } else {
        // Use STUN public address
        rtpConn, err = net.DialUDP("udp", nil, &net.UDPAddr{
            IP:   net.ParseIP(publicIP),
            Port: publicPort,
        })
    }
    if err != nil {
        log.Fatalf("Failed to create RTP connection: %v", err)
    }
    defer rtpConn.Close()

    // Start audio capture and playback (same as before)
    // ...
}

// Other functions (encodeOpus, decodeOpus, startAudioCapture, startAudioPlayback, etc.) remain unchanged