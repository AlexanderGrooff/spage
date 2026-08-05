

# Spage

Este proyecto tiene como objetivo funcionar 'como' Ansible, pero con un rendimiento mucho mayor. Al tomar un playbook de Ansible + un inventario como entrada, generará un programa en Go que puede compilarse para un host específico.
El resultado final es un archivo `.go` generado que puede compilarse y enviarse al host objetivo.

Para crear este tipo de programa, este proyecto incluye el binario `spage`, con el cual puede apuntar a playbooks + inventarios de Ansible.

Principales ventajas:

- **(Compatibilidad casi total) con Ansible** - Cualquier playbook que funcione con Ansible funciona con Spage
- **Ejecución significativamente más rápida** - Se compila a código Go nativo en lugar de interpretar Python
- **Sin dependencia de Python** - Un solo binario que puede ejecutarse en cualquier lugar
- **Características extendidas** - Ejecución paralela integrada, rollback automático ante fallos, detección de uso de variables y soporte para ejecutores externos (como `temporal`)
- **Misma sintaxis, pero con palabras clave adicionales** - Utiliza el mismo formato de playbook YAML y parámetros de módulo, con opciones adicionales para ejecución paralela con `before`/`after`

Spage funciona:

1. Tomando tus archivos de playbooks e inventarios de Ansible existentes
2. Generando código Go que implementa la misma lógica
3. Compilándolo en un solo binario para tu entorno objetivo
4. Ejecutando tareas con módulos Go nativos siempre que sea posible, recurriendo a Python para garantizar compatibilidad total

## Instalación

La forma más rápida de instalar Spage es utilizando nuestro script de instalación:

```bash
curl -sSL https://spage.dev/install.sh | bash
```

Esto detectará automáticamente tu sistema operativo y arquitectura, descargará la última versión y la instalará en `/usr/local/bin/spage`.

Para otros métodos de instalación (descarga manual, instalación con Go, compilación desde el código fuente), consulte la [Guía de Instalación](https://spage.dev/docs/installation).

## Uso

Puede usar Spage de dos formas:

1. Generar el código Go que luego puede compilar y ejecutar (usando el comando `spage generate`)
2. Ejecutar directamente en un inventario (usando el comando `spage run`)

```bash
# Generar el código Go que luego puede compilar y ejecutar
spage generate --playbook playbook.yaml --output generated_tasks.go
go build -o spage_playbook generated_tasks.go
./spage_playbook --inventory inventory.yaml

# O ejecutar directamente en un inventario
spage run --inventory inventory.yaml --playbook playbook.yaml
```

## Preguntas Frecuentes (FAQ)

### P: ¿Qué es Spage?

**R: Spage es un sustituto directo de alto rendimiento para Ansible** que compila tus playbooks en programas Go. Luego puede utilizar la cadena de herramientas de Golang para compilar y ejecutar el playbook, ya sea localmente o en el host objetivo.

### P: ¿Por qué debería usar Spage?

**R: Spage es un sustituto directo para Ansible** que compila tus playbooks en programas Go. Luego puede utilizar la cadena de herramientas de Golang para compilar y ejecutar el playbook, ya sea localmente o en el host objetivo.

Si está familiarizado con Ansible pero desea mejorar el rendimiento, compilar a un binario, deshacerse de la dependencia de Python o adoptar diferentes modelos de ejecución como ejecutar en Temporal, Spage es para usted.
En lugar de ejecutar `ansible-playbook playbook.yaml`, puede ejecutar
`spage run playbook.yaml` o `spage generate playbook.yaml -o generated_tasks.go && go run generated_tasks.go`.
Se admiten variables típicas como `--become`, `--inventory`, `--tags`, `--extra-vars`.

### P: ¿Por qué se llama Spage?

**R: Es una referencia a [Factorio: Space Age](https://www.factorio.com/space-age/buy).** Construí Spage cuando Space Age aún no se había lanzado y quería ocuparme. Así que mientras esperaba el lanzamiento, encontré un comentario muy gracioso en Reddit que lo llamaba Spage, y así nació Spage.

### P: Tengo el módulo `x.y.z` de una colección de Ansible Galaxy. ¿Está soportado en Spage?

**R: ¡Sí, absolutamente!** Spage es compatible con cualquier módulo de Ansible, incluidos aquellos de colecciones de Ansible Galaxy, a través de su mecanismo de respaldo de Python. Consulte la sección [Mecanismo de Respaldo de Python](#python-fallback-mechanism) para más detalles.

## Mecanismo de Respaldo de Python

Spage incluye un mecanismo de respaldo de Python sofisticado que le permite ejecutar cualquier módulo de Ansible, incluso aquellos no implementados nativamente en Go. Esto garantiza una compatibilidad del 100 % con el ecosistema de Ansible mientras mantiene los beneficios de rendimiento.

### Cuándo se usa el respaldo de Python

El respaldo de Python se activa automáticamente cuando:

- No se encuentra el nombre de un módulo en los módulos Go nativos de Spage
- Utiliza explícitamente el tipo de módulo `ansible_python`
- Se hacen referencia a colecciones comunitarias o módulos personalizados (p. ej., `community.general.setup`, `custom.namespace.module`)

### Cómo funciona

1. **Detección de módulo**: Spage intenta primero encontrar una implementación nativa en Go del módulo solicitado.
2. **Activación del respaldo**: Si no existe un módulo nativo, se activa el mecanismo de respaldo de Python.
3. **Generación del playbook**: Se genera un playbook de Ansible temporal que contiene el módulo requerido y sus parámetros.
4. **Ejecución remota**: Spage ejecuta el playbook en el host objetivo utilizando `ansible-playbook`.

### Ejemplos de uso

```yaml
# El módulo 'command' es compatible nativamente con Spage, por lo que no usa el respaldo de Python
- name: Get Python requirements info
  command:
    cmd: echo "Hello world!"

# El módulo de colección comunitaria no es (aún) compatible con Spage, por lo que usa el respaldo de Python
- name: Get Python requirements info
  community.general.python_requirements_info:
    dependencies: []

# El módulo de espacio de nombres personalizado también utiliza el respaldo de Python
- name: Use custom module
  my_company.custom_collection.special_module:
    param1: value1
    param2: value2

# Respaldo explícito de Python
- name: Force usage of Python fallback for ping module
  ansible_python:
    module_name: ping
    args:
      data: pong
```

### Limitaciones

- Requiere Python 3 y pip en el host de ejecución para la instalación de colecciones
- Algunos plugins complejos de Ansible pueden tener dependencias adicionales

## Diferencias entre Spage y Ansible

Spage es un sustituto directo para Ansible, pero con algunas diferencias notables:

- Los playbooks pueden comenzar sin `- tasks:`. Asume `hosts: localhost` y se ejecuta localmente.
- Las tareas se ejecutan en paralelo de forma predeterminada basándose en el uso de variables.
- Las nuevas palabras clave `before`/`after` están disponibles para controlar el flujo de tareas paralelas.
- El módulo `shell` tiene dos nuevos parámetros: `execute` y `revert`. Si no especifica estas opciones y simplemente lo usa como lo haría con Ansible, no hará nada al revertir.

POR HACER:

- Agregar condiciones de reversión `revert_when`
- ¿Deberíamos compilar activos (plantillas, archivos) junto con el código?
- `vars_prompt` en play
- `gather_facts` en play
- Lógica para `no_log`
- Soporte de plugins
- Soporte de callbacks
