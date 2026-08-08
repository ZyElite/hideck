package ikev2

type IKESAKeys struct {
	SK_d  []byte
	SK_ai []byte
	SK_ar []byte
	SK_ei []byte
	SK_er []byte
	SK_pi []byte
	SK_pr []byte
}

type ChildSAKeys struct {
	SK_ei []byte
	SK_ai []byte
	SK_er []byte
	SK_ar []byte
}
