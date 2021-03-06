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

package states

import (
	"io"

	"github.com/ontio/ontology/common"
)

type WasmContractParam struct {
	Address common.Address
	Args    []byte
}

func (this *WasmContractParam) Serialization(sink *common.ZeroCopySink) {
	sink.WriteAddress(this.Address)
	sink.WriteVarBytes([]byte(this.Args))
}

// `ContractInvokeParam.Args` has reference of `source`
func (this *WasmContractParam) Deserialization(source *common.ZeroCopySource) error {
	var irregular, eof bool

	// 根据 tx.Data 中拿到对应的合约 Address
	this.Address, eof = source.NextAddress()

	// todo 拿到对应的调用参数, 最终会被引用到 wagin-wm.Host 上面
	this.Args, _, irregular, eof = source.NextVarBytes()
	if irregular {
		return common.ErrIrregularData
	}

	if eof {
		return io.ErrUnexpectedEOF
	}
	return nil
}
