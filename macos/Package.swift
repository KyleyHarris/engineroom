// swift-tools-version:5.9
// The menu bar app for engineroom (docs/decisions.md D-008). EngineRoomKit holds everything that can be
// tested without a window: the shapes of `engineroom api` answers and the client that calls it.
// EngineRoomMenu is the window. scripts/install.sh --app builds it into an app bundle.
import PackageDescription

let package = Package(
    name: "EngineRoomMenu",
    platforms: [.macOS(.v13)],
    products: [
        .executable(name: "EngineRoomMenu", targets: ["EngineRoomMenu"]),
        .library(name: "EngineRoomKit", targets: ["EngineRoomKit"]),
    ],
    targets: [
        .target(name: "EngineRoomKit"),
        .executableTarget(name: "EngineRoomMenu", dependencies: ["EngineRoomKit"]),
        .testTarget(name: "EngineRoomKitTests", dependencies: ["EngineRoomKit"]),
    ]
)
