package sphinx

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	ec "crypto/elliptic"
	"crypto/rand"
	"encoding/gob"
	"fmt"
	scrypto "github.com/hashmatter/p3lib/sphinx/crypto"
	ma "github.com/multiformats/go-multiaddr"
	"math/big"
	"testing"
)

func TestNewPacket(t *testing.T) {
	// TODO
}

func TestNewHeader(t *testing.T) {
	numRelays := 4
	finalAddr, _ := ma.NewMultiaddr("/ip4/127.0.0.1/udp/1234")
	relayAddrsString := []string{
		"/ip4/127.0.0.1/udp/1234",
		"/ip4/198.162.0.1/tcp/4321",
		"/ip6/2607:f8b0:4003:c00::6a/udp/5678",
		// used if numRelay > 3
		"/ip4/198.162.0.2/tcp/4321",
		"/ip4/198.162.0.3/tcp/4321",
	}
	relayAddrs := make([]ma.Multiaddr, numRelays)

	circuitPrivKeys := make([]crypto.PrivateKey, numRelays)
	circuitPubKeys := make([]crypto.PublicKey, numRelays)

	privSender, _ := ecdsa.GenerateKey(ec.P256(), rand.Reader)
	//pubSender := privSender.PublicKey

	for i := 0; i < numRelays; i++ {
		pub, priv := generateHopKeys()
		circuitPrivKeys[i] = priv
		circuitPubKeys[i] = pub
		relayAddrs[i], _ = ma.NewMultiaddr(relayAddrsString[i])
	}

	header, err := constructHeader(*privSender, finalAddr, relayAddrs, circuitPubKeys)
	if err != nil {
		t.Error(err)
	}

	ri := header.RoutingInfo

	// checks if there are suffixed zeros in the padding
	count := 0
	for j := len(ri) - 1; j > 0; j-- {
		if ri[j] != 0 {
			break
		}
		count = count + 1
	}

	if count > 2 {
		t.Errorf("Header is revealing number of relays. Suffixed 0s count: %v", count)
		t.Errorf("len(routingInfo): %v | len(headerMac): %v",
			len(ri), len(header.HeaderMac))
	}
}

func TestGenSharedKeys(t *testing.T) {
	// setup
	curve := ec.P256()
	numRelays := 3
	circuitPubKeys := make([]crypto.PublicKey, numRelays)
	circuitPrivKeys := make([]crypto.PublicKey, numRelays)

	privSender, _ := ecdsa.GenerateKey(ec.P256(), rand.Reader)
	pubSender := privSender.PublicKey

	for i := 0; i < numRelays; i++ {
		pub, priv := generateHopKeys()
		circuitPrivKeys[i] = priv
		circuitPubKeys[i] = pub
	}

	// generateSharedSecrets
	sharedKeys, err := generateSharedSecrets(circuitPubKeys, *privSender)
	if err != nil {
		t.Error(err)
	}

	//e := ec.Marshal(pubSender.Curve, pubSender.X, pubSender.Y)
	//t.Error(e, len(e), pubSender.X.BitLen(), pubSender.Y.BitLen())

	// if shared keys were properly generated, the 1st hop must be able to 1)
	// generate shared key and 2) blind group element. The 2rd hop must be able to
	// generate shared key from new blind element

	// 1) first hop derives shared key, which must be the same as sharedKeys[0]
	privKey_1 := circuitPrivKeys[0].(*ecdsa.PrivateKey)
	sk_1 := scrypto.GenerateECDHSharedSecret(&pubSender, privKey_1)
	if sk_1 != sharedKeys[0] {
		t.Error(fmt.Printf("First shared key was not properly computed\n> %x\n> %x\n",
			sk_1, sharedKeys[0]))
	}

	// 2) first hop blinds group element for next hop
	blindingF := scrypto.ComputeBlindingFactor(&pubSender, sk_1)
	var privElement big.Int
	privElement.SetBytes(privKey_1.D.Bytes())
	newGroupElement := blindGroupElement(&pubSender, blindingF[:], curve)

	// 3) second hop derives shared key from blinded group element
	privKey_2 := circuitPrivKeys[1].(*ecdsa.PrivateKey)
	sk_2 := scrypto.GenerateECDHSharedSecret(newGroupElement, privKey_2)
	if sk_2 != sharedKeys[1] {
		t.Error(fmt.Printf("Second shared key was not properly computed\n> %x\n> %x\n",
			sk_2, sharedKeys[1]))
	}
}

// TODO
func TestEncodingDecodingPacket(t *testing.T) {}

func TestEncodingDecodingHeader(t *testing.T) {
	pub, _ := generateHopKeys()
	str := "dummy routing info"
	ri := [routingInfoSize]byte{}
	copy(ri[:], str[:])
	header := &Header{RoutingInfo: ri, GroupElement: pub}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	dec := gob.NewDecoder(&buf)

	err := enc.Encode(header)
	if err != nil {
		t.Error(err)
		return
	}

	var headerAfter Header
	err = dec.Decode(&headerAfter)
	if err != nil {
		t.Error(err)
		return
	}

	if string(header.RoutingInfo[:]) != string(headerAfter.RoutingInfo[:]) {
		t.Error(fmt.Printf("Original and encoded/decoded header routing info mismatch:\n >> %v \n >> %v\n",
			string(header.RoutingInfo[:]), string(headerAfter.RoutingInfo[:])))
	}

	hGe := header.GroupElement.(*ecdsa.PublicKey)
	haGe := headerAfter.GroupElement.(ecdsa.PublicKey)

	if hGe.Curve.Params().Name != haGe.Curve.Params().Name {
		t.Error(fmt.Printf("Original and encoded/decoded group elements mismatch:\n >> %v \n >> %v\n",
			hGe.Curve.Params().Name, haGe.Curve.Params().Name))
	}

	var diff big.Int
	diff.Sub(hGe.X, haGe.X)
	if diff.Cmp(big.NewInt(0)) != 0 {
		t.Error(fmt.Printf("Original and encoded/decoded group elements mismatch:\n >> %v \n >> %v\n",
			hGe.X, haGe.X))
	}
}

func TestPaddingGeneration(t *testing.T) {
	numRelays := 3
	circuitPubKeys := make([]crypto.PublicKey, numRelays)
	circuitPrivKeys := make([]crypto.PublicKey, numRelays)

	privSender, _ := ecdsa.GenerateKey(ec.P256(), rand.Reader)

	for i := 0; i < numRelays; i++ {
		pub, priv := generateHopKeys()
		circuitPrivKeys[i] = priv
		circuitPubKeys[i] = pub
	}

	// generateSharedSecrets
	sharedKeys, err := generateSharedSecrets(circuitPubKeys, *privSender)
	if err != nil {
		t.Error(err)
	}

	nonce := make([]byte, 24)
	padding, err := generatePadding(sharedKeys, nonce)
	if err != nil {
		t.Error(err)
	}

	expPaddingLen := (numRelays - 1) * relayDataSize
	if len(padding) != expPaddingLen {
		t.Error(fmt.Printf("Final padding should have lenght of |(numRelays - 1) * relaysDataSize| (%v), got %v", expPaddingLen, len(padding)))
	}

}

// helpers
func generateHopKeys() (*ecdsa.PublicKey, *ecdsa.PrivateKey) {
	privHop, _ := ecdsa.GenerateKey(ec.P256(), rand.Reader)
	pubHop := privHop.Public().(*ecdsa.PublicKey)
	return pubHop, privHop
}