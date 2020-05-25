/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */

package payload

import (
	"fmt"
	"io"

	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/errors"
)

type VmType byte

const (
	NEOVM_TYPE  VmType = 1
	WASMVM_TYPE VmType = 3
)

func VmTypeFromByte(ty byte) (VmType, error) {
	switch ty {
	case 1, 3:
		return VmType(ty), nil
	default:
		return VmType(0), fmt.Errorf("can not convert byte:%d to vm type", ty)
	}
}

// DeployCode is an implementation of transaction payload for deploy smartcontract
//
// DeployCode: 是用于部署智能合约的交易有效 data 的实现
type DeployCode struct {
	code []byte
	//0, 1 means NEOVM_TYPE, 3 means WASMVM_TYPE
	// 标识该合约code 支持的合约类型为 0: 默认支持 <NEO>, 1: 支持NEO, 3：WASM
	vmFlags     byte
	Name        string
	Version     string
	Author      string
	Email       string
	Description string

	address common.Address
}

func NewDeployCode(code []byte, vmType VmType, name, version, author, email, description string) (*DeployCode, error) {
	dc := &DeployCode{
		code:        code,
		vmFlags:     byte(vmType),
		Name:        name,
		Version:     version,
		Author:      author,
		Email:       email,
		Description: description,
	}
	err := validateDeployCode(dc)
	if err != nil {
		return nil, err
	}
	return dc, nil
}

func (dc *DeployCode) Address() common.Address {
	if dc.address == common.ADDRESS_EMPTY {
		dc.address = common.AddressFromVmCode(dc.code)
	}
	return dc.address
}

func (dc *DeployCode) GetRawCode() []byte {
	return dc.code
}

func (dc *DeployCode) GetWasmCode() ([]byte, error) {
	if dc.VmType() == WASMVM_TYPE {
		return dc.code, nil
	} else {
		return nil, errors.NewErr("not wasm contract")
	}
}

func (dc *DeployCode) GetNeoCode() ([]byte, error) {
	if dc.VmType() == NEOVM_TYPE {
		return dc.code, nil
	} else {
		return nil, errors.NewErr("not neo contract")
	}
}

func checkVmFlags(vmFlags byte) error {
	switch vmFlags {
	case 0, 1, 3:
		return nil
	default:
		return fmt.Errorf("invalid vm flags: %d", vmFlags)
	}
}

func (dc *DeployCode) VmType() VmType {
	switch dc.vmFlags {
	case 0, 1:
		return NEOVM_TYPE
	case 3:
		return WASMVM_TYPE
	default:
		// 2,或者 其他的
		panic("unreachable")
	}
}

func (dc *DeployCode) ToArray() []byte {
	sink := common.NewZeroCopySink(nil)
	dc.Serialization(sink)
	return sink.Bytes()
}

func (dc *DeployCode) Serialization(sink *common.ZeroCopySink) {
	sink.WriteVarBytes(dc.code)
	sink.WriteByte(dc.vmFlags)
	sink.WriteString(dc.Name)
	sink.WriteString(dc.Version)
	sink.WriteString(dc.Author)
	sink.WriteString(dc.Email)
	sink.WriteString(dc.Description)
}

//note: DeployCode.Code has data reference of param source
func (dc *DeployCode) Deserialization(source *common.ZeroCopySource) error {
	var eof, irregular bool

	// todo 这个是真正的 合约code
	dc.code, _, irregular, eof = source.NextVarBytes()
	if irregular {
		return common.ErrIrregularData
	}


	// todo 根据 code 中的前一小段，解出合约的虚机适配类型
	dc.vmFlags, eof = source.NextByte()

	// todo 合约的名称
	dc.Name, _, irregular, eof = source.NextString()
	if irregular {
		return common.ErrIrregularData
	}


	// todo 合约的版本
	dc.Version, _, irregular, eof = source.NextString()
	if irregular {
		return common.ErrIrregularData
	}

	// todo 合约的作者
	dc.Author, _, irregular, eof = source.NextString()
	if irregular {
		return common.ErrIrregularData
	}

	// todo 作者的email
	dc.Email, _, irregular, eof = source.NextString()
	if irregular {
		return common.ErrIrregularData
	}

	// todo 合约的描述信息
	dc.Description, _, irregular, eof = source.NextString()
	if irregular {
		return common.ErrIrregularData
	}

	if eof {
		return io.ErrUnexpectedEOF
	}

	/**
	todo ##########################
	todo ##########################
	todo ##########################
	todo
	todo 校验DeployCode
	 */
	err := validateDeployCode(dc)
	if err != nil {
		return err
	}

	return nil
}

const maxWasmCodeSize = 512 * 1024

func validateDeployCode(dep *DeployCode) error {
	err := checkVmFlags(dep.vmFlags)
	if err != nil {
		return err
	}

	if dep.VmType() == WASMVM_TYPE {
		if len(dep.code) > maxWasmCodeSize {
			return errors.NewErr("[contract] Code too long!")
		}
	} else {
		if len(dep.code) > 1024*1024 {
			return errors.NewErr("[contract] Code too long!")
		}
	}

	if len(dep.Name) > 252 {
		return errors.NewErr("[contract] name too long!")
	}

	if len(dep.Version) > 252 {
		return errors.NewErr("[contract] version too long!")
	}

	if len(dep.Author) > 252 {
		return errors.NewErr("[contract] version too long!")
	}

	if len(dep.Email) > 252 {
		return errors.NewErr("[contract] email too long!")
	}

	if len(dep.Description) > 65536 {
		return errors.NewErr("[contract] description too long!")
	}

	return nil
}

func CreateDeployCode(code []byte,
	vmType uint32,
	name []byte,
	version []byte,
	author []byte,
	email []byte,
	desc []byte) (*DeployCode, error) {
	if vmType > 255 {
		return nil, fmt.Errorf("wrong vm flags: %d", vmType)
	}

	contract := &DeployCode{
		code:        code,
		vmFlags:     byte(vmType),
		Name:        string(name),
		Version:     string(version),
		Author:      string(author),
		Email:       string(email),
		Description: string(desc),
	}

	err := validateDeployCode(contract)
	if err != nil {
		return nil, err
	}
	return contract, nil
}
