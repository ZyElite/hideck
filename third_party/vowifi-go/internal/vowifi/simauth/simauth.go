package simauth

import enginesim "github.com/iniwex5/vowifi-go/engine/sim"

type AKAResult = enginesim.AKAResult

type AKAProvider interface {
	CalculateAKA(rand16, autn16 []byte) (AKAResult, error)
}
