    if ($Mode -eq 'Set') {
        Set-SmbServerConfiguration -EnableSMB1Protocol $false -Force
        Write-Done
        return
    }
    $enabled = (Get-SmbServerConfiguration).EnableSMB1Protocol
    if ($enabled) { Write-Result $false 'SMBv1 está activado.' } else { Write-Result $true 'SMBv1 desactivado.' }
