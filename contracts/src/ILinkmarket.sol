// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

interface ILinkmarket {
    event NewMarket(uint256 indexed id, bytes32 requestHash);
    event OutcomeRequested(uint256 indexed id);
    event MarketOutcome(uint256 indexed id, bool indexed outcome);

    struct Market {
        bytes32 requestHash;
        bytes32 donID;
        uint256 expiry;
        address yes;
        address no;
        bool outcome;
    }

    function price(int128 qA, int128 qB) external view returns (int128);

    function cost(
        int128 delta,
        int128 qA,
        int128 qB
    ) external view returns (uint256);

    function mintYes(
        uint256 id,
        int128 delta,
        address receiver
    ) external payable;

    function mintNo(
        uint256 id,
        int128 delta,
        address receiver
    ) external payable;

    function newMarket(
        bytes32 requestHash,
        bytes32 donID,
        uint256 expiry
    ) external returns (uint256 id);

    function requestOutcome(
        uint256 id,
        bytes memory request,
        uint64 subscriptionId,
        uint32 gasLimit
    ) external returns (bytes32 requestId);
}
