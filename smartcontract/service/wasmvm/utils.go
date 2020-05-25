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
package wasmvm

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/go-interpreter/wagon/exec"
	"github.com/go-interpreter/wagon/validate"
	"github.com/go-interpreter/wagon/wasm"
)

/**
todo 根据 ptr <内存偏移量>和 len <内存长度>从wagon的memory中获取对应的内容
 */
func ReadWasmMemory(proc *exec.Process, ptr uint32, len uint32) ([]byte, error) {

	// todo ptr的起始地址位置 <某个长度索引位> +总共占用的长度
	//		如果超过了 memory 的总占用长度则太大了
	if uint64(proc.MemSize()) < uint64(ptr)+uint64(len) {
		return nil, errors.New("contract create len is greater than memory size")
	}
	// 根据占用长度，起一个等长的 bytes
	keybytes := make([]byte, len)
	// 根据ptr索引去填充 bytes 的内容
	_, err := proc.ReadAt(keybytes, int64(ptr))
	if err != nil {
		return nil, err
	}

	return keybytes, nil
}

func checkOntoWasm(m *wasm.Module) error {
	if m.Start != nil {
		return errors.New("[Validate] start section is not allowed.")
	}

	if m.Export == nil {
		return errors.New("[Validate] No export in wasm!")
	}

	if len(m.Export.Entries) != 1 {
		return errors.New("[Validate] Can only export one entry.")
	}

	entry, ok := m.Export.Entries["invoke"]
	if ok == false {
		return errors.New("[Validate] invoke entry function does not export.")
	}

	if entry.Kind != wasm.ExternalFunction {
		return errors.New("[Validate] Can only export invoke function entry.")
	}

	//get entry index
	index := int64(entry.Index)
	//get function index
	fidx := m.Function.Types[int(index)]
	//get  function type
	ftype := m.Types.Entries[int(fidx)]

	if len(ftype.ReturnTypes) > 0 {
		return errors.New("[Validate] ExecCode error! Invoke function return sig error")
	}
	if len(ftype.ParamTypes) > 0 {
		return errors.New("[Validate] ExecCode error! Invoke function param sig error")
	}

	return nil
}

func ReadWasmModule(Code []byte, verify bool) (*exec.CompiledModule, error) {

	// todo 先获取 一个 module
	// todo
	// todo #############################
	// todo #############################
	// todo #############################
	//
	// TODO 这个就是调用了 wagon 的 wasm.ReadModule()
	//
	// 这里就是和 PlatON 不一样的地方， 本体是根据 一个回调匿名函数
	m, err := wasm.ReadModule(bytes.NewReader(Code), func(name string) (*wasm.Module, error) {

		// todo 为啥 `env` 字符啊
		switch name {
		case "env":
			return NewHostModule(), nil
		}
		return nil, fmt.Errorf("module %q unknown", name)
	})


	if err != nil {
		return nil, err
	}

	if verify {
		err = checkOntoWasm(m)
		if err != nil {
			return nil, err
		}

		err = validate.VerifyModule(m)
		if err != nil {
			return nil, err
		}

		err = validate.VerifyWasmCodeFromRust(Code)
		if err != nil {
			return nil, err
		}
	}

	compiled, err := exec.CompileModule(m)
	if err != nil {
		return nil, err
	}

	return compiled, nil
}
