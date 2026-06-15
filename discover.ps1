Write-Host "Querying mDNS for all HDHomeRun devices..." -ForegroundColor Cyan

# Force Windows to gather all IP addresses claiming the generic mDNS name
$mDnsRecords = Resolve-DnsName -Name hdhomerun.local -Type A -ErrorAction SilentlyContinue

if ($mDnsRecords) {
    # Extract just the unique IPv4 addresses
    $ips = $mDnsRecords | Select-Object -ExpandProperty IPAddress -Unique

    foreach ($ip in $ips) {
        try {
            $device = Invoke-RestMethod -Uri "http://$ip/discover.json" -TimeoutSec 2 -ErrorAction Stop
            
            # Add the raw IP to the object for your convenience
            $device | Add-Member -MemberType NoteProperty -Name "LocalIP" -Value $ip
            
            $device | Select-Object FriendlyName, ModelNumber, DeviceID, LocalIP, BaseURL
        } catch {
            Write-Warning "Could not retrieve JSON from $ip"
        }
    }
} else {
    Write-Warning "No devices responded to the mDNS query."
}