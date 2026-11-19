    # La cuenta de invitado se identifica por su RID 501, no por su nombre (que cambia con el idioma).
    $guest = Get-LocalUser | Where-Object { $_.SID.Value -match '^S-1-5-21-.*-501$' } | Select-Object -First 1
    if ($null -eq $guest) {
        Write-Result $true 'No existe la cuenta de invitado.'
        return
    }
    if ($Mode -eq 'Set') {
        Disable-LocalUser -SID $guest.SID
        Write-Done
        return
    }
    if ($guest.Enabled) { Write-Result $false "La cuenta $($guest.Name) está activada." } else { Write-Result $true 'Cuenta de invitado desactivada.' }
