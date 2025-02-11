// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Clones} from "@openzeppelin/contracts/proxy/Clones.sol";
import {IERC1363Receiver} from "@openzeppelin/contracts/interfaces/IERC1363Receiver.sol";

import {FunctionsClient} from "@chainlink/contracts/src/v0.8/functions/v1_0_0/FunctionsClient.sol";
import {ConfirmedOwner} from "@chainlink/contracts/src/v0.8/shared/access/ConfirmedOwner.sol";
import {FunctionsRequest} from "@chainlink/contracts/src/v0.8/functions/v1_0_0/libraries/FunctionsRequest.sol";

import {ABDKMath64x64} from "abdk-libraries-solidity/ABDKMath64x64.sol";

import {LinkmarketERC20, ILinkmarketERC20} from "./LinkmarketERC20.sol";
import {ILinkmarket} from "./ILinkmarket.sol";

contract Linkmarket is FunctionsClient, IERC1363Receiver, ILinkmarket {
    using ABDKMath64x64 for int128;

    uint256 public constant OUTCOME_DURATION = 5 minutes;

    int128 public immutable PRICE_SCALE;

    int128 public immutable LIQUIDITY;

    address public immutable tokenImplementation;

    mapping(uint256 => Market) private _markets;
    uint256 private _totalMarkets;

    // Request ID => Market ID
    mapping(bytes32 => uint256) private _requestedMarkets;

    constructor(address router) FunctionsClient(router) {
        tokenImplementation = address(new LinkmarketERC20());

        PRICE_SCALE = ABDKMath64x64.div(
            ABDKMath64x64.fromUInt(1),
            ABDKMath64x64.fromUInt(5000)
        );

        LIQUIDITY = ABDKMath64x64.fromUInt(1);
    }

    receive() external payable {}

    function onTransferReceived(
        address /* operator */,
        address /* from */,
        uint256 value,
        bytes calldata data
    ) external override returns (bytes4) {
        (uint256 id, address receiver) = abi.decode(data, (uint256, address));

        _redeem(id, msg.sender, value, receiver);

        return bytes4(keccak256("onTransferReceived(address,address,uint256,bytes)"));
    }

    function price(int128 qA, int128 qB) public view override returns (int128) {
        int128 expYes = (qA.div(LIQUIDITY)).exp();
        int128 expNo = (qB.div(LIQUIDITY)).exp();
        int128 sumExp = expYes.add(expNo);
        int128 lnSum = sumExp.ln();

        return LIQUIDITY.mul(lnSum).sub(LIQUIDITY.mul(ABDKMath64x64.ln(2)));
    }

    function cost(
        int128 delta,
        int128 qA,
        int128 qB
    ) public view override returns (uint256) {
        int128 oldCost = price(qA, qB);
        int128 newCost = price(qA.add(delta), qB);
        int128 costDiff = newCost.sub(oldCost);
        int128 scaledCostDiff = costDiff.mul(PRICE_SCALE);

        return ABDKMath64x64.mulu(scaledCostDiff, 1 ether);
    }

    function mintYes(
        uint256 id,
        int128 delta,
        address receiver
    ) external payable override {
        Market memory market = _markets[id];

        uint256 mintCost = cost(
            delta,
            ABDKMath64x64.div(
                ABDKMath64x64.fromUInt(
                    ILinkmarketERC20(market.yes).totalSupply()
                ),
                ABDKMath64x64.fromUInt(1e18)
            ),
            ABDKMath64x64.div(
                ABDKMath64x64.fromUInt(
                    ILinkmarketERC20(market.no).totalSupply()
                ),
                ABDKMath64x64.fromUInt(1e18)
            )
        );
        if (msg.value < mintCost) {
            revert("Insufficient payment");
        }

        uint256 mintAmount = ABDKMath64x64.mulu(delta, 1e18);
        ILinkmarketERC20(market.yes).mint(receiver, mintAmount);
    }

    function mintNo(
        uint256 id,
        int128 delta,
        address receiver
    ) external payable override {
        Market memory market = _markets[id];

        uint256 mintCost = cost(
            delta,
            ABDKMath64x64.div(
                ABDKMath64x64.fromUInt(
                    ILinkmarketERC20(market.no).totalSupply()
                ),
                ABDKMath64x64.fromUInt(1e18)
            ),
            ABDKMath64x64.div(
                ABDKMath64x64.fromUInt(
                    ILinkmarketERC20(market.yes).totalSupply()
                ),
                ABDKMath64x64.fromUInt(1e18)
            )
        );
        if (msg.value < mintCost) {
            revert("Insufficient payment");
        }

        uint256 mintAmount = ABDKMath64x64.mulu(delta, 1e18);
        ILinkmarketERC20(market.no).mint(receiver, mintAmount);
    }

    function redeem(uint256 id, address token, uint256 value, address receiver) external {
        ILinkmarketERC20(token).transferFrom(msg.sender, address(this), value);

        _redeem(id, token, value, receiver);
    }

    function _redeem(uint256 id, address token, uint256 value, address receiver) internal {
        if (id >= _totalMarkets) {
            revert("");
        }

        Market memory market = _markets[id];

        if (market.expiry != 0) {
            revert("");
        }

        if(market.outcome && token != market.yes) {
            revert("");
        }

        if(!market.outcome && token != market.no) {
            revert("");
        }

        ILinkmarketERC20(token).burn(value);

        // Withdraw ETH
    }

    function newMarket(
        bytes32 requestHash,
        bytes32 donID,
        uint256 expiry
    ) external override returns (uint256 id) {
        if (expiry <= block.timestamp) {
            revert("");
        }

        id = _totalMarkets;
        _totalMarkets++;

        address yes = Clones.clone(tokenImplementation);
        address no = Clones.clone(tokenImplementation);

        _markets[id] = Market(requestHash, donID, expiry, yes, no, false);

        emit NewMarket(id, requestHash);
    }

    function requestOutcome(
        uint256 id,
        bytes memory request,
        uint64 subscriptionId,
        uint32 gasLimit
    ) external override returns (bytes32 requestId) {
        Market memory market = _markets[id];

        if (keccak256(request) != market.requestHash) {
            revert("");
        }

        if (
            block.timestamp < market.expiry ||
            block.timestamp > market.expiry + OUTCOME_DURATION
        ) {
            revert("");
        }

        requestId = _sendRequest(
            request,
            subscriptionId,
            gasLimit,
            market.donID
        );

        emit OutcomeRequested(id);
    }

    function fulfillRequest(
        bytes32 requestId,
        bytes memory response,
        bytes memory err
    ) internal override {
        if (err.length > 0) {
            return;
        }

        uint256 id = _requestedMarkets[requestId] - 1;
        Market memory market = _markets[id];

        if (
            block.timestamp < market.expiry ||
            block.timestamp > market.expiry + OUTCOME_DURATION
        ) {
            return;
        }

        bool outcome = abi.decode(response, (bool));
        _markets[id].outcome = outcome;

        _markets[id].expiry = 0;

        emit MarketOutcome(id, outcome);
    }
}
