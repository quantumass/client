// Copyright 2015 Keybase, Inc. All rights reserved. Use of
// this source code is governed by the included BSD license.

package engine

import (
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/keybase/client/go/kex2"
	"github.com/keybase/client/go/libkb"
	keybase1 "github.com/keybase/client/go/protocol"
	"golang.org/x/net/context"
)

type fakeSaltPackUI struct{}

func (s fakeSaltPackUI) SaltPackPromptForDecrypt(_ context.Context, arg keybase1.SaltPackPromptForDecryptArg) (err error) {
	return nil
}

func (s fakeSaltPackUI) SaltPackVerifySuccess(_ context.Context, arg keybase1.SaltPackVerifySuccessArg) error {
	return nil
}

func TestSaltPackDecrypt(t *testing.T) {
	tc := SetupEngineTest(t, "SaltPackDecrypt")
	defer tc.Cleanup()
	fu := CreateAndSignupFakeUser(tc, "naclp")

	// encrypt a message
	msg := "10 days in Japan"
	sink := libkb.NewBufferCloser()
	ctx := &Context{
		IdentifyUI: &FakeIdentifyUI{},
		SecretUI:   fu.NewSecretUI(),
		LogUI:      tc.G.UI.GetLogUI(),
		SaltPackUI: &fakeSaltPackUI{},
	}
	// Should encrypt for self, too.
	arg := &SaltPackEncryptArg{
		Source: strings.NewReader(msg),
		Sink:   sink,
	}
	enc := NewSaltPackEncrypt(arg, tc.G)
	if err := RunEngine(enc, ctx); err != nil {
		t.Fatal(err)
	}
	out := sink.String()

	t.Logf("encrypted data: %s", out)

	// decrypt it
	decoded := libkb.NewBufferCloser()
	decarg := &SaltPackDecryptArg{
		Source: strings.NewReader(out),
		Sink:   decoded,
	}
	dec := NewSaltPackDecrypt(decarg, tc.G)
	if err := RunEngine(dec, ctx); err != nil {
		t.Fatal(err)
	}
	decmsg := decoded.String()
	if decmsg != msg {
		t.Errorf("decoded: %s, expected: %s", decmsg, msg)
	}

	pgpMsg := `-----BEGIN PGP MESSAGE-----
Version: GnuPG v1

hQEMA5gKPw0B/gTfAQf+JacZcP+4d1cdmRV5qlrDUhK3qm5dtzAh8KE3z6OMSOmE
fUAdMZweHZMkWA5C1OZbvZ6SKaFLFHjmiD0DWlcdiXsvgPH9RpTHOSrxdjRlBuwK
JBz5OrDM/OStIam6jKcxBcrI43JkWOG64AOwJ4Rx3OjAnzbKJKeUCAaopbXc2M5O
iyTPzEsexRFjSfPGRk9cQD5zfar3Qjk2cRWElgABiQczWtfNAQ3NyQLzmRU6mw+i
ZLoViAwQm2BMYa2i6MYOJCQtxHLwZCtAbRXTGFZ2nP0gVVX50KIeL/rnzrQ4I05M
CljEVk3BBSQBl3jqecfT2Ooh+rwgf3VSQ684HIEt5dI/Aama8l7S3ypwVyt8gWhN
HTngZWUk8Tjn6Q8zrnnoB92G1G+rZHAiChgBFQCaYDBsWa0Pia6Vm+10OAIulGGj
=pNG+
-----END PGP MESSAGE-----
`
	decoded = libkb.NewBufferCloser()
	decarg = &SaltPackDecryptArg{
		Source: strings.NewReader(pgpMsg),
		Sink:   decoded,
	}
	dec = NewSaltPackDecrypt(decarg, tc.G)
	err := RunEngine(dec, ctx)
	if wse, ok := err.(libkb.WrongCryptoFormatError); !ok {
		t.Fatalf("Wanted a WrongCryptoFormat error, but got %T (%v)", err, err)
	} else if wse.Wanted != libkb.CryptoMessageFormatSaltPack ||
		wse.Received != libkb.CryptoMessageFormatPGP ||
		wse.Operation != "decrypt" {
		t.Fatalf("Bad error: %v", wse)
	}

}

type testDecryptSaltPackUI struct {
	fakeSaltPackUI
	f func(arg keybase1.SaltPackPromptForDecryptArg) error
}

func (t *testDecryptSaltPackUI) SaltPackPromptForDecrypt(_ context.Context, arg keybase1.SaltPackPromptForDecryptArg) (err error) {
	if t.f == nil {
		return nil
	}
	return t.f(arg)
}

func TestSaltPackDecryptBrokenTrack(t *testing.T) {

	tc := SetupEngineTest(t, "SaltPackDecrypt")
	defer tc.Cleanup()

	// create a user to track the proofUser
	trackUser := CreateAndSignupFakeUser(tc, "naclp")
	Logout(tc)

	// create a user with a rooter proof
	proofUser := CreateAndSignupFakeUser(tc, "naclp")
	ui, _, err := proveRooter(tc.G, proofUser)
	if err != nil {
		t.Fatal(err)
	}

	spui := testDecryptSaltPackUI{}

	// encrypt a message
	msg := "10 days in Japan"
	sink := libkb.NewBufferCloser()
	ctx := &Context{
		IdentifyUI: &FakeIdentifyUI{},
		SecretUI:   proofUser.NewSecretUI(),
		LogUI:      tc.G.UI.GetLogUI(),
		SaltPackUI: &spui,
	}

	arg := &SaltPackEncryptArg{
		Source: strings.NewReader(msg),
		Sink:   sink,
		Opts: keybase1.SaltPackEncryptOptions{
			NoSelfEncrypt: true,
			Recipients: []string{
				trackUser.Username,
			},
		},
	}
	enc := NewSaltPackEncrypt(arg, tc.G)
	if err := RunEngine(enc, ctx); err != nil {
		t.Fatal(err)
	}
	out := sink.String()

	// Also output a hidden-sender message
	arg.Opts.HideSelf = true
	sink = libkb.NewBufferCloser()
	arg.Source = strings.NewReader(msg)
	arg.Sink = sink
	enc = NewSaltPackEncrypt(arg, tc.G)
	if err := RunEngine(enc, ctx); err != nil {
		t.Fatal(err)
	}
	outHidden := sink.String()

	Logout(tc)

	// Now login as the track user and track the proofUser
	trackUser.LoginOrBust(tc)
	rbl := sb{
		social:     true,
		id:         proofUser.Username + "@rooter",
		proofState: keybase1.ProofState_OK,
	}
	outcome := keybase1.IdentifyOutcome{
		NumProofSuccesses: 1,
		TrackStatus:       keybase1.TrackStatus_NEW_OK,
	}
	err = checkTrack(tc, trackUser, proofUser.Username, []sb{rbl}, &outcome)
	if err != nil {
		t.Fatal(err)
	}

	// decrypt it
	decoded := libkb.NewBufferCloser()
	decarg := &SaltPackDecryptArg{
		Source: strings.NewReader(out),
		Sink:   decoded,
	}
	dec := NewSaltPackDecrypt(decarg, tc.G)
	spui.f = func(arg keybase1.SaltPackPromptForDecryptArg) error {
		if arg.Sender.SenderType != keybase1.SaltPackSenderType_TRACKING_OK {
			t.Fatalf("Bad sender type: %v", arg.Sender.SenderType)
		}
		return nil
	}
	if err := RunEngine(dec, ctx); err != nil {
		t.Fatal(err)
	}
	decmsg := decoded.String()
	// Should work!
	if decmsg != msg {
		t.Fatalf("decoded: %s, expected: %s", decmsg, msg)
	}

	// now decrypt the hidden-self message
	decoded = libkb.NewBufferCloser()
	decarg = &SaltPackDecryptArg{
		Source: strings.NewReader(outHidden),
		Sink:   decoded,
	}
	dec = NewSaltPackDecrypt(decarg, tc.G)
	spui.f = func(arg keybase1.SaltPackPromptForDecryptArg) error {
		if arg.Sender.SenderType != keybase1.SaltPackSenderType_ANONYMOUS {
			t.Fatalf("Bad sender type: %v", arg.Sender.SenderType)
		}
		return nil
	}
	if err := RunEngine(dec, ctx); err != nil {
		t.Fatal(err)
	}
	decmsg = decoded.String()
	// Should work!
	if decmsg != msg {
		t.Fatalf("decoded: %s, expected: %s", decmsg, msg)
	}

	// remove the rooter proof to break the tracking statement
	Logout(tc)
	proofUser.LoginOrBust(tc)
	if err := proveRooterRemove(tc.G, ui.postID); err != nil {
		t.Fatal(err)
	}

	Logout(tc)

	// Decrypt the message and fail, since our tracking statement is now
	// broken.
	trackUser.LoginOrBust(tc)
	decoded = libkb.NewBufferCloser()
	decarg = &SaltPackDecryptArg{
		Source: strings.NewReader(out),
		Sink:   decoded,
		Opts: keybase1.SaltPackDecryptOptions{
			ForceRemoteCheck: true,
		},
	}
	dec = NewSaltPackDecrypt(decarg, tc.G)
	errTrackingBroke := errors.New("tracking broke")
	spui.f = func(arg keybase1.SaltPackPromptForDecryptArg) error {
		if arg.Sender.SenderType != keybase1.SaltPackSenderType_TRACKING_BROKE {
			t.Fatalf("Bad sender type: %v", arg.Sender.SenderType)
		}
		return errTrackingBroke
	}
	if err = RunEngine(dec, ctx); err != errTrackingBroke {
		t.Fatalf("Expected an error %v; but got %v", errTrackingBroke, err)
	}
}

func TestSaltPackNoEncryptionForDevice(t *testing.T) {

	// device X (provisioner) context:
	tcX := SetupEngineTest(t, "kex2provision")
	defer tcX.Cleanup()

	// device Y (provisionee) context:
	tcY := SetupEngineTest(t, "kex2provionee")
	defer tcY.Cleanup()

	// device Z is the encryptor's device
	tcZ := SetupEngineTest(t, "encryptor")
	defer tcZ.Cleanup()

	// provisioner needs to be logged in
	userX := CreateAndSignupFakeUser(tcX, "naclp")
	var secretX kex2.Secret
	if _, err := rand.Read(secretX[:]); err != nil {
		t.Fatal(err)
	}

	encryptor := CreateAndSignupFakeUser(tcZ, "naclp")
	spui := testDecryptSaltPackUI{}

	// encrypt a message with encryption / tcZ
	msg := "10 days in Japan"
	sink := libkb.NewBufferCloser()
	ctx := &Context{
		IdentifyUI: &FakeIdentifyUI{},
		SecretUI:   encryptor.NewSecretUI(),
		LogUI:      tcZ.G.UI.GetLogUI(),
		SaltPackUI: &spui,
	}

	arg := &SaltPackEncryptArg{
		Source: strings.NewReader(msg),
		Sink:   sink,
		Opts: keybase1.SaltPackEncryptOptions{
			Recipients: []string{
				userX.Username,
			},
		},
	}
	enc := NewSaltPackEncrypt(arg, tcZ.G)
	if err := RunEngine(enc, ctx); err != nil {
		t.Fatal(err)
	}
	out := sink.String()

	// decrypt it with userX / tcX
	decoded := libkb.NewBufferCloser()
	decarg := &SaltPackDecryptArg{
		Source: strings.NewReader(out),
		Sink:   decoded,
	}
	dec := NewSaltPackDecrypt(decarg, tcX.G)
	spui.f = func(arg keybase1.SaltPackPromptForDecryptArg) error {
		if arg.Sender.SenderType != keybase1.SaltPackSenderType_NOT_TRACKED {
			t.Fatalf("Bad sender type: %v", arg.Sender.SenderType)
		}
		return nil
	}
	ctx = &Context{
		IdentifyUI: &FakeIdentifyUI{},
		SecretUI:   userX.NewSecretUI(),
		LogUI:      tcX.G.UI.GetLogUI(),
		SaltPackUI: &spui,
	}

	if err := RunEngine(dec, ctx); err != nil {
		t.Fatal(err)
	}
	decmsg := decoded.String()
	// Should work!
	if decmsg != msg {
		t.Fatalf("decoded: %s, expected: %s", decmsg, msg)
	}

	// Now make a new device
	secretCh := make(chan kex2.Secret)

	// provisionee calls login:
	ctx = &Context{
		ProvisionUI: newTestProvisionUISecretCh(secretCh),
		LoginUI:     &libkb.TestLoginUI{},
		LogUI:       tcY.G.UI.GetLogUI(),
		SecretUI:    &libkb.TestSecretUI{},
		GPGUI:       &gpgtestui{},
	}
	eng := NewLogin(tcY.G, libkb.DeviceTypeDesktop, "", keybase1.ClientType_CLI)

	var wg sync.WaitGroup

	// start provisionee
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := RunEngine(eng, ctx); err != nil {
			t.Errorf("login error: %s", err)
			return
		}
	}()

	// start provisioner
	provisioner := NewKex2Provisioner(tcX.G, secretX, nil)
	wg.Add(1)
	go func() {
		defer wg.Done()

		ctx := &Context{
			SecretUI:    userX.NewSecretUI(),
			ProvisionUI: newTestProvisionUI(),
		}
		if err := RunEngine(provisioner, ctx); err != nil {
			t.Errorf("provisioner error: %s", err)
			return
		}
	}()
	secretFromY := <-secretCh
	provisioner.AddSecret(secretFromY)

	wg.Wait()

	if err := AssertProvisioned(tcY); err != nil {
		t.Fatal(err)
	}

	// Now try and fail to decrypt with device Y (via tcY)
	decoded = libkb.NewBufferCloser()
	decarg = &SaltPackDecryptArg{
		Source: strings.NewReader(out),
		Sink:   decoded,
	}
	dec = NewSaltPackDecrypt(decarg, tcY.G)
	spui.f = func(arg keybase1.SaltPackPromptForDecryptArg) error {
		t.Fatal("should not be prompted for decryption")
		return nil
	}
	ctx = &Context{
		IdentifyUI: &FakeIdentifyUI{},
		SecretUI:   userX.NewSecretUI(),
		LogUI:      tcY.G.UI.GetLogUI(),
		SaltPackUI: &spui,
	}

	if err := RunEngine(dec, ctx); err == nil {
		t.Fatal("Should have seen a decryption error")
	}

	// Make sure we get the right helpful debug message back
	me := dec.MessageInfo()
	if len(me.Devices) != 2 {
		t.Fatalf("expected 2 valid devices; got %d", len(me.Devices))
	}

	backup := 0
	desktops := 0
	for _, d := range me.Devices {
		switch d.Type {
		case "backup":
			backup++
		case "desktop":
			desktops++
			if !userX.EncryptionKey.GetKID().Equal(d.EncryptKey) {
				t.Fatal("got wrong encryption key for good possibilities")
			}
		default:
			t.Fatalf("wrong kind of device: %s\n", d.Type)
		}
	}
	if backup != 1 {
		t.Fatal("Only wanted 1 backup key")
	}
	if desktops != 1 {
		t.Fatal("only wanted 1 desktop key")
	}
}
