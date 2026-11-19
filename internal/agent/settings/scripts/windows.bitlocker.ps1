    if (-not (Get-Command Get-BitLockerVolume -ErrorAction SilentlyContinue)) {
        Write-Result $false 'BitLocker no está disponible en esta edición de Windows.' $true
        return
    }
    $drive = $env:SystemDrive
    $vol = Get-BitLockerVolume -MountPoint $drive
    $escrow = Test-Flag 'ESCROW_KEY' $true
    $recoveryProtector = $vol.KeyProtector | Where-Object { $_.KeyProtectorType -eq 'RecoveryPassword' } | Select-Object -First 1

    if ($Mode -eq 'Set') {
        if ($vol.VolumeStatus -eq 'FullyDecrypted') {
            $tpm = Get-Tpm
            if (-not $tpm.TpmPresent -or -not $tpm.TpmReady) {
                throw 'El equipo no tiene un TPM preparado: no se puede activar BitLocker sin intervención.'
            }
            Enable-BitLocker -MountPoint $drive -TpmProtector -UsedSpaceOnly -SkipHardwareTest | Out-Null
        }
        elseif ($vol.ProtectionStatus -eq 'Off') {
            Resume-BitLocker -MountPoint $drive | Out-Null
        }
        if ($null -eq $recoveryProtector) {
            Add-BitLockerKeyProtector -MountPoint $drive -RecoveryPasswordProtector | Out-Null
        }
        Write-Done
        return
    }

    if ($vol.VolumeStatus -eq 'FullyDecrypted') {
        $tpm = Get-Tpm
        if (-not $tpm.TpmPresent) {
            Write-Result $false 'El equipo no tiene TPM.' $true
            return
        }
    }
    $recovery = $null
    if ($escrow -and $null -ne $recoveryProtector) {
        $recovery = @{ volume = $vol.MountPoint; key_id = $recoveryProtector.KeyProtectorId; recovery_password = $recoveryProtector.RecoveryPassword }
    }
    $protected = $vol.ProtectionStatus -eq 'On'
    $detail = "Unidad $($vol.MountPoint), protección $($vol.ProtectionStatus), estado $($vol.VolumeStatus) ($($vol.EncryptionPercentage) %)."
    if ($protected -and $null -eq $recoveryProtector) {
        Write-Result $false ($detail + ' Falta la contraseña de recuperación.') $false $recovery
        return
    }
    Write-Result $protected $detail $false $recovery
