// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {ERC20Burnable} from "@openzeppelin/contracts/token/ERC20/extensions/ERC20Burnable.sol";
import {ERC20Permit, IERC20Permit} from "@openzeppelin/contracts/token/ERC20/extensions/ERC20Permit.sol";
import {ERC1363} from "@openzeppelin/contracts/token/ERC20/extensions/ERC1363.sol";

import {ILinkmarketERC20} from "./ILinkmarketERC20.sol";

contract LinkmarketERC20 is
    ERC20,
    ERC20Burnable,
    ERC20Permit,
    ERC1363,
    ILinkmarketERC20
{
    address private _owner;

    string private _name;
    string private _symbol;

    constructor() ERC20("", "") ERC20Permit("Linkmarket") {}

    function initialize(
        address initOwner,
        string memory initName,
        string memory initSymbol
    ) external override {
        _owner = initOwner;

        _name = initName;
        _symbol = initSymbol;
    }

    function nonces(
        address owner
    ) public view override(ERC20Permit, IERC20Permit) returns (uint256) {
        return super.nonces(owner);
    }

    function name() public view override returns (string memory) {
        return _name;
    }

    function symbol() public view override returns (string memory) {
        return _symbol;
    }

    function mint(address account, uint256 value) external override {
        if (msg.sender != _owner) {
            revert("");
        }

        _mint(account, value);
    }

    function burn(
        uint256 value
    ) public override(ERC20Burnable, ILinkmarketERC20) {
        super.burn(value);
    }

    function burnFrom(
        address account,
        uint256 value
    ) public override(ERC20Burnable, ILinkmarketERC20) {
        super.burnFrom(account, value);
    }
}
