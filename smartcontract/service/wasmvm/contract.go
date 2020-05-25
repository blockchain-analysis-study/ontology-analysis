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
	"github.com/go-interpreter/wagon/exec"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/core/payload"
	"github.com/ontio/ontology/errors"
)
// todo 合约创建
func ContractCreate(proc *exec.Process,
	codePtr uint32,
	codeLen uint32,
	vmType uint32,
	namePtr uint32,
	nameLen uint32,
	verPtr uint32,
	verLen uint32,
	authorPtr uint32,
	authorLen uint32,
	emailPtr uint32,
	emailLen uint32,
	descPtr uint32,
	descLen uint32,
	newAddressPtr uint32) uint32 {
	self := proc.HostData().(*Runtime)
	code, err := ReadWasmMemory(proc, codePtr, codeLen)
	if err != nil {
		panic(err)
	}

	cost := CONTRACT_CREATE_GAS + uint64(uint64(codeLen)/PER_UNIT_CODE_LEN)*UINT_DEPLOY_CODE_LEN_GAS
	self.checkGas(cost)

	name, err := ReadWasmMemory(proc, namePtr, nameLen)
	if err != nil {
		panic(err)
	}

	version, err := ReadWasmMemory(proc, verPtr, verLen)
	if err != nil {
		panic(err)
	}

	author, err := ReadWasmMemory(proc, authorPtr, authorLen)
	if err != nil {
		panic(err)
	}

	email, err := ReadWasmMemory(proc, emailPtr, emailLen)
	if err != nil {
		panic(err)
	}

	desc, err := ReadWasmMemory(proc, descPtr, descLen)
	if err != nil {
		panic(err)
	}

	dep, err := payload.CreateDeployCode(code, vmType, name, version, author, email, desc)
	if err != nil {
		panic(err)
	}

	wasmCode, err := dep.GetWasmCode()
	if err != nil {
		panic(err)
	}
	_, err = ReadWasmModule(wasmCode, true)
	if err != nil {
		panic(err)
	}

	contractAddr := dep.Address()
	if self.isContractExist(contractAddr) {
		panic(errors.NewErr("contract has been deployed"))
	}

	self.Service.CacheDB.PutContract(dep)

	length, err := proc.WriteAt(contractAddr[:], int64(newAddressPtr))
	return uint32(length)

}

/**
todo 合约迁移 <合约升级>
 */
func ContractMigrate(proc *exec.Process, // wagon的process结构，是传递给host functions 以访问诸如内存和控制之类的代理。
	codePtr uint32, // 新contract code的在wasm内存中的偏移量
	codeLen uint32, // 新contract code的在wasm内存中的长度
	vmType uint32,  // 需要用到的vm类型
	namePtr uint32, // 新合约的name在wasm内存中的偏移量
	nameLen uint32, // 新合约的name在wasm内存中的长度
	verPtr uint32,  // 新合约的版本在wasm内存中的偏移量
	verLen uint32,  // 新合约的版本在wasm内存中的长度
	authorPtr uint32, // 新合约的作者在wasm内存中的偏移量
	authorLen uint32, // 新合约的作者在wasm内存中的长度
	emailPtr uint32,  // 新合约的email在wasm内存中的偏移量
	emailLen uint32,  // 新合约的email在wasm内存中的长度
	descPtr uint32,   // 新合约的描述在wasm内存中的偏移量
	descLen uint32,   // 新合约的描述在wasm内存中的长度
	newAddressPtr uint32 /* 新合约的地址 */) uint32 {

	self := proc.HostData().(*Runtime)

	// todo 使用对应的ptr <内存偏移量>和len <内存长度>
	code, err := ReadWasmMemory(proc, codePtr, codeLen)
	if err != nil {
		panic(err)
	}

	// 检查 新合约的 code 消耗的gas
	cost := CONTRACT_CREATE_GAS + uint64(uint64(codeLen)/PER_UNIT_CODE_LEN)*UINT_DEPLOY_CODE_LEN_GAS
	self.checkGas(cost)

	name, err := ReadWasmMemory(proc, namePtr, nameLen)
	if err != nil {
		panic(err)
	}

	version, err := ReadWasmMemory(proc, verPtr, verLen)
	if err != nil {
		panic(err)
	}

	author, err := ReadWasmMemory(proc, authorPtr, authorLen)
	if err != nil {
		panic(err)
	}

	email, err := ReadWasmMemory(proc, emailPtr, emailLen)
	if err != nil {
		panic(err)
	}

	desc, err := ReadWasmMemory(proc, descPtr, descLen)
	if err != nil {
		panic(err)
	}

	// todo 组装 deployCode
	dep, err := payload.CreateDeployCode(code, vmType, name, version, author, email, desc)
	if err != nil {
		panic(err)
	}

	// todo 从deployCode中获取真正的 code
	wasmCode, err := dep.GetWasmCode()
	if err != nil {
		panic(err)
	}

	/**
	todo 有根据 新的code 加载了下 module

	todo 这里为什么，不使用新code 实例化的新 module ?
	 */
	_, err = ReadWasmModule(wasmCode, true)
	if err != nil {
		panic(err)
	}

	// todo 拿到新合约地址
	contractAddr := dep.Address()
	if self.isContractExist(contractAddr) {
		panic(errors.NewErr("contract has been deployed"))
	}

	// todo 从当前执行下下文中获取当前旧合约地址
	oldAddress := self.Service.ContextRef.CurrentContext().ContractAddress

	// todo 存储新合约
	self.Service.CacheDB.PutContract(dep)
	//todo 删除旧合约
	self.Service.CacheDB.DeleteContract(oldAddress)


	// todo 获取 旧合约的迭代器
	iter := self.Service.CacheDB.NewIterator(oldAddress[:])
	for has := iter.First(); has; has = iter.Next() {
		key := iter.Key()
		val := iter.Value()

		// todo 逐个遍历 旧合约的  K-V， 并用旧 Key 和新合约addr组装成 新Key
		newkey := serializeStorageKey(contractAddr, key[20:])

		// todo 将旧的 value转移到新的Key上
		self.Service.CacheDB.Put(newkey, val)
		// todo 删除旧的 Key
		self.Service.CacheDB.Delete(key)
	}

	// 释放迭代器
	iter.Release()
	if err := iter.Error(); err != nil {
		panic(err)
	}


	// 将新合约的 地址写回到 memory中
	length, err := proc.WriteAt(contractAddr[:], int64(newAddressPtr))
	if err != nil {
		panic(err)
	}

	return uint32(length)
}


// todo 合约销毁
func ContractDestroy(proc *exec.Process) {
	self := proc.HostData().(*Runtime)

	// todo 从当前合约上下文中获取当前合约的 addr
	contractAddress := self.Service.ContextRef.CurrentContext().ContractAddress
	iter := self.Service.CacheDB.NewIterator(contractAddress[:])
	// todo 遍历当前合约的 迭代器，逐个清除掉 K-V
	for has := iter.First(); has; has = iter.Next() {
		self.Service.CacheDB.Delete(iter.Key())
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		panic(err)
	}

	// todo 删除掉合约
	self.Service.CacheDB.DeleteContract(contractAddress)
	//the contract has been deleted ,quit the contract operation
	// todo 终止当前合约对应的 module的执行
	proc.Terminate()
}

func (self *Runtime) isContractExist(contractAddress common.Address) bool {
	item, err := self.Service.CacheDB.GetContract(contractAddress)
	if err != nil {
		panic(err)
	}
	return item != nil
}
