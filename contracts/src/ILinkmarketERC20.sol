// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {IERC20Permit} from "@openzeppelin/contracts/token/ERC20/extensions/IERC20Permit.sol";
import {IERC1363} from "@openzeppelin/contracts/interfaces/IERC1363.sol";

interface ILinkmarketERC20 is IERC20, IERC20Permit, IERC1363 {
    function initialize(
        address initOwner,
        string memory initName,
        string memory initSymbol
    ) external;

    function mint(address account, uint256 value) external;

    function burn(uint256 value) external;
    function burnFrom(address account, uint256 value) external;
}
