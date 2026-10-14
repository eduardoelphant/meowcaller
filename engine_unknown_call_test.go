package meowcaller

import (
	"context"
	"strings"
	"testing"

	"github.com/purpshell/meowcaller/signaling"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

func rawCallControl(tag, callID string, creator types.JID) waBinary.Node {
	action := waBinary.Node{
		Tag:   tag,
		Attrs: waBinary.Attrs{"call-id": callID, "call-creator": creator},
	}
	switch tag {
	case "enc_rekey":
		action.Attrs["transaction-id"] = "2"
		action.Content = []waBinary.Node{
			{Tag: "encopt", Attrs: waBinary.Attrs{"keygen": "2"}},
			{Tag: "enc", Attrs: waBinary.Attrs{"v": "2", "type": "pkmsg", "count": "0"}, Content: []byte{1, 2, 3}},
		}
	case "video":
		action.Attrs["state"] = "11"
	}
	return waBinary.Node{
		Tag: "call",
		Attrs: waBinary.Attrs{
			"from": types.NewJID("300", types.HiddenUserServer), "id": "STANZA", "t": "1",
		},
		Content: []waBinary.Node{action},
	}
}

func TestRawCallControlForUnknownCallIsLeftToWhatsmeow(t *testing.T) {
	// The engine holds GROUP; every stanza below names a call it never took.
	// testGroupEngine has no whatsmeow client, so an attempted enc_rekey decrypt
	// would panic.
	for _, tag := range []string{"group_update", "enc_rekey", "waiting_room_update", "user_action", "screen_share", "video"} {
		t.Run(tag, func(t *testing.T) {
			eng, _, creator := testGroupEngine("GROUP")
			var sent []waBinary.Node
			eng.sendCallNode = func(_ context.Context, node waBinary.Node) error {
				sent = append(sent, node)
				return nil
			}
			node := rawCallControl(tag, "OTHER", creator)
			if eng.onCallRaw(&node) {
				t.Fatal("onCallRaw claimed a stanza for an unknown call")
			}
			if len(sent) != 0 {
				t.Fatalf("sent %d nodes (typed ack) for an unknown call", len(sent))
			}
			if _, ok := eng.calls["OTHER"]; ok {
				t.Fatal("engine created state for an unknown call")
			}
		})
	}
}

func TestVideoStanzaForKnownCallStillSendsTypedAck(t *testing.T) {
	eng, _, creator := testGroupEngine("GROUP")
	var sent []waBinary.Node
	eng.sendCallNode = func(_ context.Context, node waBinary.Node) error {
		sent = append(sent, node)
		return nil
	}
	node := rawCallControl("video", "GROUP", creator)
	if !eng.onCallRaw(&node) {
		t.Fatal("known video stanza was not handled")
	}
	if len(sent) == 0 || sent[0].Tag != "ack" || sent[0].AttrGetter().OptionalString("type") != "video" {
		t.Fatalf("sent %#v, want a type=video ack first", sent)
	}
}

func TestGroupRekeyForUnknownCallSkipsDecrypt(t *testing.T) {
	eng, _, creator := testGroupEngine("GROUP")
	node := rawCallControl("enc_rekey", "OTHER", creator)
	envelope := parseEnvelopeForTest(t, &node)
	// No whatsmeow client: reaching DecryptDM would panic.
	err := eng.ingestGroupEpoch(context.Background(), envelope)
	if err == nil || !strings.Contains(err.Error(), "unknown call") {
		t.Fatalf("ingestGroupEpoch err = %v, want unknown call", err)
	}
}

func parseEnvelopeForTest(t *testing.T, node *waBinary.Node) *signaling.CallControlEnvelope {
	t.Helper()
	envelope, err := signaling.ParseCallControlEnvelope(node)
	if err != nil {
		t.Fatalf("ParseCallControlEnvelope: %v", err)
	}
	return envelope
}
