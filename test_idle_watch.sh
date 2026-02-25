#!/bin/bash

AXCTL="./axctl"
TIMEOUT=5000

echo "=== Monitor de Actividad / Inactividad ==="
echo "Configurado para: $((TIMEOUT / 1000)) segundos"
echo "Presiona Ctrl+C para salir"
echo "------------------------------------------"

while true; do
    echo "👁️  Esperando inactividad de $((TIMEOUT / 1000))s... (Deja de tocar el teclado/ratón)"
    
    # Esto bloqueará hasta que pase el tiempo configurado sin actividad
    $AXCTL system input-idle-wait $TIMEOUT
    
    # Cuando pasamos esta línea, significa que se detectó la inactividad
    echo "💤 [$(date +%H:%M:%S)] ¡Inactividad detectada! Sistema en idle."
    
    echo "🖱️  Esperando actividad... (Mueve el ratón para continuar)"
    
    # Esto bloqueará hasta que haya actividad de nuevo
    $AXCTL system input-resume-wait $TIMEOUT
    
    # Cuando pasamos esta línea, significa que se detectó actividad
    echo "⚡ [$(date +%H:%M:%S)] ¡Actividad detectada! Has vuelto."
    echo "------------------------------------------"
done
