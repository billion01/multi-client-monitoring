// Copyright 2017 Maarten H. Everts and Tim R. van de Kamp.
// All rights reserved.
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

// Package crypmonsys is an implementation of the proposed scheme in the
// paper “Multi-client Predicate-only Encryption for Conjunctive
// Equality Tests.”
package crypmonsys

import (
	"crypto/sha256"
	"errors"
	"github.com/Nik-U/pbc"
)

// SystemParameters holds the system parameters of the scheme. This includes
// pairing and the generators used.
type SystemParameters struct {
	g1, g2  *pbc.Element
	pairing *pbc.Pairing
}

// F implements a Pseudorandom Function (PRF) based on [NR04] that maps an input
// message to an element in either group 1 or group 2.
// Please note that this function is definitely NOT implemented as a timing safe
// function!
func (sp *SystemParameters) F(group int, base *pbc.Element, beta []*pbc.Element, aux *pbc.Element, input int32) *pbc.Element {
	br := sp.pairing.NewZr().Set1()
	// Divide x by 2 (bitshift to right) until at zero
	for x, i := input, 0; x > 0; i, x = i+1, x>>1 {
		if x%2 == 1 {
			br.ThenMulZn(beta[i])
		}
	}
	// The usage of aux is a small optimization that can reduce the number of
	// exponentiations.
	br.ThenMulZn(aux)

	var result *pbc.Element

	switch group {
	case 1:
		result = sp.pairing.NewG1()
	case 2:
		result = sp.pairing.NewG2()
	default:
		panic("Group should be either 1 or 2.")
	}

	return result.PowZn(base, br)
}

// NewSystemParameters generates and returns new system parameters based on the
// provided pairing.
func NewSystemParameters(pairing *pbc.Pairing) *SystemParameters {
	return &SystemParameters{
		g1:      pairing.NewG1().Rand(),
		g2:      pairing.NewG2().Rand(),
		pairing: pairing,
	}
}

// NewSystemParametersFromFile reads system parameters from a file.
// TODO
func NewSystemParametersFromFile(filename string) *SystemParameters {
	panic("Unimplemented!")
}

// SetupPart holds information (keys) about an agent needed in the setup
// algorithm.
type SetupPart struct {
	alpha *pbc.Element
	beta  []*pbc.Element
	gamma *pbc.Element
}

// SetupKey holds all the information to generate key material for the Agents
// and the Rule generator.
type SetupKey struct {
	keys []SetupPart
	sp   *SystemParameters
}

// Agent represents an agent in the system. It has all the information (keys
// etc.) to be able to generate ciphertexts.
type Agent struct {
	index   int
	g1alpha *pbc.Element
	beta    []*pbc.Element
	gamma   *pbc.Element
	sp      *SystemParameters
}

// Ciphertext holds a ciphertext generated by an Agent.
type Ciphertext struct {
	index        int
	part1, part2 *pbc.Element
}

// NewCiphertext creates a new ciphertext of a message that is attached to a
// specific identifier.
func (a *Agent) NewCiphertext(identifier string, plaintext int32) *Ciphertext {
	hID := a.sp.pairing.NewG1().SetFromStringHash(identifier, sha256.New())
	r := a.sp.pairing.NewZr().Rand()

	// Compute g1^r
	ct1 := a.sp.pairing.NewG1().PowZn(a.sp.g1, r)
	// ct2 = F(SK1, beta, x)^r * H(ID)^\gamma
	ct2 := a.sp.F(1, a.g1alpha, a.beta, r, plaintext).ThenMul(a.sp.pairing.NewG1().PowZn(hID, a.gamma))

	return &Ciphertext{index: a.index, part1: ct1, part2: ct2}
}

// AgentInfo holds information about the Agent with which a Rule Generator can
// generate rules that use the status of that Agent.
type AgentInfo struct {
	g2alpha *pbc.Element
	beta    []*pbc.Element
	g2gamma *pbc.Element
}

// RuleGenerator represents a rule generator that can generate rule over the
// agents it knows about.
type RuleGenerator struct {
	agents []AgentInfo
	sp     *SystemParameters
}

// RuleToken represents an encrypted rule (= token) defined over the output
// (status) of a set of agents.
type RuleToken struct {
	indices []int
	g2u     []*pbc.Element
	f2u     []*pbc.Element
	product *pbc.Element
}

var (
	// ErrWrongNumberOfRules is an error that is issued when the supplied rule
	// does not match the number of agents.
	ErrWrongNumberOfRules = errors.New("Number of components in the rule does not match number of agents.")
)

// NewToken generates a new rule token. The rules are passed along in the form
// of a slice of integers. Negative numbers represent a wildcard.
func (rg *RuleGenerator) NewToken(rules []int32) (*RuleToken, error) {
	if len(rules) < len(rg.agents) {
		return nil, ErrWrongNumberOfRules
	}
	r := &RuleToken{
		indices: make([]int, 0, len(rules)),
		g2u:     make([]*pbc.Element, 0, len(rules)),
		f2u:     make([]*pbc.Element, 0, len(rules)),
		// Initialized to 1 as we will multiply it with something for each rule.
		product: rg.sp.pairing.NewG2().Set1(),
	}

	for i, v := range rules {
		// For now, when the value of rule is negative it is considered a wildcard
		if v >= 0 {
			r.indices = append(r.indices, i)
			u := rg.sp.pairing.NewZr().Rand()
			r.g2u = append(r.g2u, rg.sp.pairing.NewG2().PowZn(rg.sp.g2, u))
			// r.f2u = append(r.f2u, rg.sp.pairing.NewG2().PowZn(rg.sp.F(2, rg.agents[i].g2alpha, rg.agents[i].beta, v), u))
			r.f2u = append(r.f2u, rg.sp.F(2, rg.agents[i].g2alpha, rg.agents[i].beta, u, v))
			// TODO: Check what is more efficient, as it is written now or the following:
			// r.F2u = append(r.F2u, rg.sp.F2(rg.sp.pairing.NewG2().PowZn(rg.agents[i].g2alpha, u), rg.agents[i].beta, y))
			r.product.ThenMul(rg.sp.pairing.NewG2().PowZn(rg.agents[i].g2gamma, u))
		}
	}
	return r, nil
}

// NewSetupKey generates a new setup key based on the provided system parameters.
func NewSetupKey(sp *SystemParameters) *SetupKey {
	return &SetupKey{
		keys: make([]SetupPart, 0, 10),
		sp:   sp,
	}
}

// GenerateKeys generates keys for the rule generator and the agents (for the
// setup algorithm).
func (sk *SetupKey) GenerateKeys(n, messageSpaceBitSize int) (rg *RuleGenerator, agents []*Agent) {
	rg = &RuleGenerator{sp: sk.sp}
	agents = make([]*Agent, n)
	rg.agents = make([]AgentInfo, n)

	for i := 0; i < n; i++ {
		alpha := sk.sp.pairing.NewZr().Rand()
		beta := make([]*pbc.Element, messageSpaceBitSize)
		for j := 0; j < messageSpaceBitSize; j++ {
			beta[j] = sk.sp.pairing.NewZr().Rand()
		}
		gamma := sk.sp.pairing.NewZr().Rand()
		agents[i] = &Agent{
			index:   i,
			g1alpha: sk.sp.pairing.NewG1().PowZn(sk.sp.g1, alpha),
			beta:    beta,
			gamma:   gamma,
			sp:      sk.sp}
		rg.agents[i] = AgentInfo{
			g2alpha: sk.sp.pairing.NewG2().PowZn(sk.sp.g2, alpha),
			beta:    beta,
			g2gamma: sk.sp.pairing.NewG2().PowZn(sk.sp.g2, gamma),
		}
	}
	return
}

// AlarmSystem represents the system that tests whether token and ciphertexts
// match, without being able to see the content of the rules nor messages.
type AlarmSystem struct {
	sp  *SystemParameters
	rt  *RuleToken
	hID *pbc.Element
}

// NewAlarmSystem creates a new alarm system.
func NewAlarmSystem(sp *SystemParameters, rt *RuleToken, identifier string) *AlarmSystem {
	return &AlarmSystem{
		sp:  sp,
		rt:  rt,
		hID: sp.pairing.NewG1().SetFromStringHash(identifier, sha256.New()),
	}
}

// Test is a function that tests whether the provided ciphertexts match the
// token defined for the AlarmSystem.
func (as *AlarmSystem) Test(ct []*Ciphertext) bool {
	parts1 := make([]*pbc.Element, len(as.rt.indices))
	parts2 := make([]*pbc.Element, len(as.rt.indices))
	for i, v := range as.rt.indices {
		parts1[i], parts2[i] = ct[v].part1, ct[v].part2
	}
	p1 := as.sp.pairing.NewGT().ProdPairSlice(parts1, as.rt.f2u)
	p1.ThenMul(as.sp.pairing.NewGT().Pair(as.hID, as.rt.product))
	p2 := as.sp.pairing.NewGT().ProdPairSlice(parts2, as.rt.g2u)
	return p1.Equals(p2)
}
