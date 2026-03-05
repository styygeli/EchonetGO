package model

// GetResProperty is one EPC response (EPC + PDC + EDT) from an ECHONET Get_Res.
type GetResProperty struct {
	EPC byte
	PDC byte
	EDT []byte
}
