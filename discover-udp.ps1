<#
.SYNOPSIS
    Discovers HDHomeRun devices on the local network via UDP broadcast.
.DESCRIPTION
    Sends an HDHomeRun discovery request (HDHOMERUN_TYPE_DISCOVER_REQ)
    over UDP broadcast to port 65001. It listens for replies from local tuners
    and parses the payload to extract the Device ID and IP Address.
#>

$ErrorActionPreference = 'Stop'

function Get-HDHomeRunCRC32 {
    param ([byte[]]$Data)
    [uint32]$crc = 0xFFFFFFFFu
    foreach ($byte in $Data) {
        $crc = $crc -bxor $byte
        for ($i = 0; $i -lt 8; $i++) {
            if (($crc -band 1) -ne 0) {
                $crc = ($crc -shr 1) -bxor 0xEDB88320u
            } else {
                $crc = $crc -shr 1
            }
        }
    }
    return $crc -bxor 0xFFFFFFFFu
}

function Find-HDHomeRunDevices {
    param (
        [int]$TimeoutMs = 2000,
        [string]$BroadcastIP = "255.255.255.255"
    )

    $Port = 65001

    # HDHOMERUN_TYPE_DISCOVER_REQ Payload (Wildcard match)
    $PacketData = [byte[]]@(
        0x00, 0x02,                         # Packet Type: DISCOVER_REQ (0x0002)
        0x00, 0x0c,                         # Payload Length: 12 bytes
        0x01, 0x04, 0xff, 0xff, 0xff, 0xff, # Tag 0x01 (Device Type), Length: 4, Value: 0xFFFFFFFF
        0x02, 0x04, 0xff, 0xff, 0xff, 0xff  # Tag 0x02 (Device ID), Length: 4, Value: 0xFFFFFFFF
    )

    # Calculate 32-bit Ethernet CRC and append to packet (must be little-endian)
    $crc32 = Get-HDHomeRunCRC32 -Data $PacketData
    $crcBytes = [BitConverter]::GetBytes($crc32)
    if (-not [BitConverter]::IsLittleEndian) { [Array]::Reverse($crcBytes) }
    $RequestPacket = $PacketData + $crcBytes

    # Initialize UDP Client
    $UdpClient = New-Object System.Net.Sockets.UdpClient
    $UdpClient.EnableBroadcast = $true
    $UdpClient.Client.ReceiveTimeout = $TimeoutMs

    $Endpoint = New-Object System.Net.IPEndPoint([System.Net.IPAddress]::Parse($BroadcastIP), $Port)

    Write-Host "Broadcasting HDHomeRun discovery request on UDP port $Port..."
    $UdpClient.Send($RequestPacket, $RequestPacket.Length, $Endpoint) | Out-Null

    $ListenEndpoint = New-Object System.Net.IPEndPoint([System.Net.IPAddress]::Any, 0)
    $FoundDevices = @()

    try {
        while ($true) {
            $Response = $UdpClient.Receive([ref]$ListenEndpoint)
            
            # Check for HDHOMERUN_TYPE_DISCOVER_RPY (0x0003)
            if ($Response.Length -ge 4 -and $Response[0] -eq 0x00 -and $Response[1] -eq 0x03) {
                
                $deviceId = "Unknown"
                
                # Payload length is Big-Endian
                $payloadLength = ($Response[2] -shl 8) -bor $Response[3]
                $idx = 4
                
                # Parse TLV (Tag-Length-Value) Payload
                while ($idx -lt ($payloadLength + 4) -and $idx -lt ($Response.Length - 4)) {
                    $tag = $Response[$idx]
                    $len = $Response[$idx + 1]
                    $tagHeaderSize = 2

                    # Handle 2-byte lengths (spec: MSB set indicates 2-byte length)
                    if (($len -band 0x80) -ne 0) {
                        $lenBytes2 = $Response[$idx + 2]
                        $len = ($len -band 0x7F) -bor ($lenBytes2 -shl 7)
                        $tagHeaderSize = 3
                    }

                    # Extract Device ID (Tag 0x02)
                    if ($tag -eq 0x02 -and $len -eq 4) {
                        $deviceId = [System.BitConverter]::ToString($Response[($idx + $tagHeaderSize)..($idx + $tagHeaderSize + 3)]).Replace("-", "")
                    }
                    
                    $idx += $tagHeaderSize + $len
                }

                $FoundDevices += [PSCustomObject]@{
                    DeviceID  = $deviceId
                    IPAddress = $ListenEndpoint.Address.ToString()
                }
            }
        }
    }
    catch [System.Management.Automation.MethodInvocationException] {
        # ReceiveTimeout exception indicates the end of the listening period
    }
    finally {
        $UdpClient.Close()
    }

    return $FoundDevices
}

# Run the function and format the output
Find-HDHomeRunDevices | Format-Table -AutoSize