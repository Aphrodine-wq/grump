const https = require("https");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { execSync } = require("child_process");
const zlib = require("zlib");

const REPO = "Aphrodine-wq/grump";
const VERSION = require("../package.json").version;
const BINARY_NAME = "grump";

const PLATFORM_MAP = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const ARCH_MAP = {
  x64: "amd64",
  arm64: "arm64",
};

function getDownloadURL() {
  const platform = PLATFORM_MAP[process.platform];
  const arch = ARCH_MAP[process.arch];

  if (!platform) {
    throw new Error(`Unsupported platform: ${process.platform}`);
  }
  if (!arch) {
    throw new Error(`Unsupported architecture: ${process.arch}`);
  }

  const ext = process.platform === "win32" ? "zip" : "tar.gz";
  const tag = `v${VERSION}`;
  return `https://github.com/${REPO}/releases/download/${tag}/${BINARY_NAME}_${tag}_${platform}_${arch}.${ext}`;
}

function download(url) {
  return new Promise((resolve, reject) => {
    const request = (url) => {
      https
        .get(url, { headers: { "User-Agent": "@grump/cli" } }, (res) => {
          if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
            request(res.headers.location);
            return;
          }
          if (res.statusCode !== 200) {
            reject(new Error(`Download failed: HTTP ${res.statusCode} from ${url}`));
            return;
          }
          const chunks = [];
          res.on("data", (chunk) => chunks.push(chunk));
          res.on("end", () => resolve(Buffer.concat(chunks)));
          res.on("error", reject);
        })
        .on("error", reject);
    };
    request(url);
  });
}

function extractTarGz(buffer, destDir, binaryName) {
  const tmpFile = path.join(os.tmpdir(), `grump-${Date.now()}.tar.gz`);
  fs.writeFileSync(tmpFile, buffer);

  try {
    execSync(`tar -xzf "${tmpFile}" -C "${destDir}"`, { stdio: "pipe" });

    // The binary might be at the root or inside a directory
    const binPath = path.join(destDir, binaryName);
    if (!fs.existsSync(binPath)) {
      // Search for it in subdirectories
      const files = fs.readdirSync(destDir, { recursive: true });
      for (const file of files) {
        const fullPath = path.join(destDir, file.toString());
        if (path.basename(fullPath) === binaryName && fs.statSync(fullPath).isFile()) {
          fs.renameSync(fullPath, binPath);
          break;
        }
      }
    }
  } finally {
    fs.unlinkSync(tmpFile);
  }
}

function extractZip(buffer, destDir, binaryName) {
  const tmpFile = path.join(os.tmpdir(), `grump-${Date.now()}.zip`);
  fs.writeFileSync(tmpFile, buffer);

  try {
    if (process.platform === "win32") {
      execSync(
        `powershell -Command "Expand-Archive -Path '${tmpFile}' -DestinationPath '${destDir}' -Force"`,
        { stdio: "pipe" }
      );
    } else {
      execSync(`unzip -o "${tmpFile}" -d "${destDir}"`, { stdio: "pipe" });
    }

    const binPath = path.join(destDir, binaryName);
    if (!fs.existsSync(binPath)) {
      const files = fs.readdirSync(destDir, { recursive: true });
      for (const file of files) {
        const fullPath = path.join(destDir, file.toString());
        if (path.basename(fullPath) === binaryName && fs.statSync(fullPath).isFile()) {
          fs.renameSync(fullPath, binPath);
          break;
        }
      }
    }
  } finally {
    fs.unlinkSync(tmpFile);
  }
}

async function main() {
  const url = getDownloadURL();
  const binDir = path.join(__dirname, "..", "bin");
  const binaryName = process.platform === "win32" ? `${BINARY_NAME}.exe` : BINARY_NAME;

  console.log(`Downloading grump ${VERSION} for ${process.platform}-${process.arch}...`);

  try {
    const buffer = await download(url);

    if (process.platform === "win32") {
      extractZip(buffer, binDir, binaryName);
    } else {
      extractTarGz(buffer, binDir, binaryName);
    }

    // Ensure the binary is executable
    const binPath = path.join(binDir, binaryName);
    if (!fs.existsSync(binPath)) {
      throw new Error("Binary not found after extraction");
    }

    if (process.platform !== "win32") {
      fs.chmodSync(binPath, 0o755);
    }

    console.log("grump installed successfully!");
  } catch (err) {
    console.error("Failed to install grump:", err.message);
    console.error("");
    console.error("You can manually download the binary from:");
    console.error(`  https://github.com/${REPO}/releases/tag/v${VERSION}`);
    process.exit(1);
  }
}

main();
