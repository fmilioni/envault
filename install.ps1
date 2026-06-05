# envault installer for Windows.
#   irm https://raw.githubusercontent.com/fmilioni/envault/main/install.ps1 | iex
#
# Env vars:
#   ENVAULT_VERSION   install a specific tag (default: latest release)
#   ENVAULT_BASE_URL  override the download base (testing; bypasses VERSION)
$ErrorActionPreference = "Stop"

# Windows PowerShell 5.1 may default to TLS 1.0; GitHub requires 1.2+.
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$repo = "fmilioni/envault"
$binary = "envault"

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
	"AMD64" { "amd64" }
	"ARM64" { "arm64" }
	default { throw "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)" }
}
$asset = "${binary}_windows_${arch}.exe"

if ($env:ENVAULT_BASE_URL) {
	$base = $env:ENVAULT_BASE_URL
} elseif ($env:ENVAULT_VERSION) {
	$base = "https://github.com/$repo/releases/download/$($env:ENVAULT_VERSION)"
} else {
	$base = "https://github.com/$repo/releases/latest/download"
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
	$assetPath = Join-Path $tmp $asset
	$sumsPath = Join-Path $tmp "checksums.txt"
	Write-Host "Downloading $asset from $base ..."
	Invoke-WebRequest -Uri "$base/$asset" -OutFile $assetPath -UseBasicParsing
	Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $sumsPath -UseBasicParsing

	$line = Select-String -Path $sumsPath -Pattern ([regex]::Escape($asset)) | Select-Object -First 1
	if (-not $line) { throw "no checksum entry for $asset" }
	$expected = ($line.Line -split '\s+')[0].ToLower()
	$actual = (Get-FileHash -Algorithm SHA256 -Path $assetPath).Hash.ToLower()
	if ($expected -ne $actual) { throw "checksum mismatch — corrupt download" }

	$dir = Join-Path $env:LOCALAPPDATA "Envault"
	New-Item -ItemType Directory -Path $dir -Force | Out-Null
	$target = Join-Path $dir "$binary.exe"
	Move-Item -Force $assetPath $target
	Unblock-File $target # clear mark-of-the-web so the binary runs without a prompt
	Write-Host "Installed $binary to $target"
	try { & $target --version } catch {}

	# Add to the user PATH only when absent from Machine+User scopes (no duplicates).
	$onPath = @(
		[Environment]::GetEnvironmentVariable("Path", "Machine"),
		[Environment]::GetEnvironmentVariable("Path", "User")
	) -join ';' -split ';' | Where-Object { $_ -ne "" }
	if ($onPath -notcontains $dir) {
		# Write through the registry preserving REG_EXPAND_SZ, otherwise
		# SetEnvironmentVariable bakes in any %VAR% entries the user has.
		$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Environment", $true)
		try {
			$raw = $key.GetValue("Path", "", [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
			$newRaw = if ([string]::IsNullOrEmpty($raw)) { $dir } else { $raw.TrimEnd(';') + ';' + $dir }
			$key.SetValue("Path", $newRaw, [Microsoft.Win32.RegistryValueKind]::ExpandString)
		} finally {
			$key.Close()
		}
		Write-Host "Added $dir to your user PATH."
	}
	$env:Path = "$env:Path;$dir" # current session, so 'envault' works right away
	Write-Host "Open a new terminal for 'envault' to be available everywhere."
} finally {
	Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
