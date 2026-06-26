
### Deployment Guide: `stremio-easynews-service-alpine.md`

# Deploying Stremio Easynews Go Addon as an OpenRC Service on Alpine Linux (Proxmox LXC)

This guide provides step-by-step instructions to deploy the statically compiled Go-based Stremio Easynews Addon as a background system service on an Alpine Linux LXC (Linux Container) under Proxmox VE. 

We will adhere to Alpine best practices by housing the execution daemon in `/etc/init.d/` and its companion configuration variables in `/etc/conf.d/` [3]. The service runs securely under an unprivileged system user.

---

## Prerequisites

1. **Alpine Linux LXC** running on Proxmox VE.
2. Root or `sudo` access inside the container.
3. Your compiled `stremio-easynews` binary (see compilation steps below).

---

## Step 1: Compiling the Portable Binary

Alpine Linux uses `musl-libc` instead of `glibc`. To ensure absolute portability and avoid shared library loading errors, you must compile your Go application as a statically linked binary.

Run this compilation command from your development machine (or inside the container if Go is installed):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o stremio-easynews cmd/addon/main.go
```
* `CGO_ENABLED=0` ensures the binary has zero external C-library dependencies.
* `-trimpath` removes local filesystem paths from the compiled binary.
* `-ldflags="-s -w"` strips debugging symbols and debugging tables, reducing the binary size by over 40%.

---

## Step 2: Preparing the LXC Container Environment

For security best practices, the service must not run as `root`. We will create a dedicated, unprivileged system user and group named `easynews`.

1. Log into your Alpine LXC.
2. Create the system group and user:
   ```bash
   addgroup -S easynews
   adduser -S -G easynews -h /var/lib/stremio-easynews -s /sbin/nologin easynews
   ```
3. Move your compiled `stremio-easynews` binary to a standard system binaries directory and make it executable:
   ```bash
   mv stremio-easynews /usr/local/bin/
   chmod +x /usr/local/bin/stremio-easynews
   ```

---

## Step 3: Creating the Service Configuration (`/etc/conf.d/`)

In Alpine Linux, service-specific variables must reside in `/etc/conf.d/stremio-easynews` [3]. This file supports standard `KEY=VALUE` pairs, blank lines, and comments starting with `#`.

1. Create and open the configuration file:
   ```bash
   nano /etc/conf.d/stremio-easynews
   ```
2. Populate the file with your configuration. You can copy, paste, and adapt the following template:
   ```ini
   # ===========================================================================
   # Stremio Easynews Addon Service Configuration
   # Place key=value pairs here. Comments and empty lines are safely ignored.
   # ===========================================================================

   PORT=1337
   EASYNEWS_LOG_LEVEL=info
   EASYNEWS_SUMMARIZE_LOGS=true

   # Your public instance URL (used for stream resolution redirection)
   ADDON_BASE_URL=https://abcd.duckdns.org   # could be the localhost or ip with port format too. stremio looks for https url , nuvio might work with ip without https

   # Optional TMDB API Key for dynamic alternative titles and localized translations
   TMDB_API_KEY=your_tmdb_api_key_here

   ADDON_CONFIG_KEY="base64 32length key"              #use in linux shell  "openssl rand -base64 32" paste that value #if not key set, the addon would use plaintext user/pass in the url
   # Search Limits
   TOTAL_MAX_RESULTS=500
   MAX_RESULTS_PER_PAGE=250
   MAX_PAGES=10
   CACHE_TTL=24
   ```
3. Secure the configuration file so only the root and the `easynews` service account can read it:
   ```bash
   chown root:easynews /etc/conf.d/stremio-easynews
   chmod 640 /etc/conf.d/stremio-easynews
   ```

---

## Step 4: Creating the OpenRC Service Script

Now, we will write a custom OpenRC init script that dynamically parses `/etc/conf.d/stremio-easynews`, exports its `key=value` parameters into the service's environment, and starts the daemon under the unprivileged `easynews` user [3].

1. Create a new service script at `/etc/init.d/stremio-easynews`:
   ```bash
   nano /etc/init.d/stremio-easynews
   ```
2. Paste the following complete, production-grade script:
   ```sh
   #!/sbin/openrc-run

   name="stremio-easynews"
   description="Stremio Easynews Go Addon Service"

   # Service execution parameters
   command="/usr/local/bin/stremio-easynews"
   command_background="yes"
   pidfile="/run/${RC_SVCNAME}.pid"
   command_user="easynews:easynews"
   
   # Stderr and Stdout redirections
   output_log="/var/log/stremio-easynews.log"
   error_log="/var/log/stremio-easynews.err"

   depend() {
       need net
   }

   start_pre() {
       # Ensure runtime log files exist and are writable by the unprivileged user
       touch "$output_log" "$error_log"
       chown easynews:easynews "$output_log" "$error_log"

       # Dynamically parse and export environment variables from the standard Alpine conf.d location
       if [ -f "/etc/conf.d/stremio-easynews" ]; then
           while read -r line || [ -n "$line" ]; do
               # Trim leading and trailing whitespace
               trimmed=$(echo "$line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
               
               # Skip empty lines and lines starting with '#'
               case "$trimmed" in
                   ""|#*) continue ;;
               esac
               
               # Extract valid KEY=VALUE pairs
               if echo "$trimmed" | grep -q "="; then
                   # Separate key and value
                   key=$(echo "$trimmed" | cut -d'=' -f1 | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
                   val=$(echo "$trimmed" | cut -d'=' -f2- | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
                   
                   # Strip surrounding single or double quotes if present
                   val=$(echo "$val" | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "'$//")
                   
                   # Export variables into the context of the spawning daemon
                   export "$key=$val"
               fi
           done < "/etc/conf.d/stremio-easynews"
       fi
   }
   ```
3. Make the script executable:
   ```bash
   chmod +x /etc/init.d/stremio-easynews
   ```

---

## Step 5: Managing the Service

You can now register, start, and manage your new service using Alpine's native service management commands.

* **Register the service to start automatically on system boot**:
  ```bash
  rc-update add stremio-easynews default
  ```

* **Start the service**:
  ```bash
  rc-service stremio-easynews start
  ```

* **Check the service status**:
  ```bash
  rc-service stremio-easynews status
  ```

* **Restart the service** (run this whenever you modify `/etc/conf.d/stremio-easynews`):
  ```bash
  rc-service stremio-easynews restart
  ```

* **Stop the service**:
  ```bash
  rc-service stremio-easynews stop
  ```

---

## Step 6: Verifying Logs and Troubleshooting

If the service fails to start or you need to inspect incoming requests and latency timings, read the custom log destinations created by the OpenRC script:

* **Inspect standard output logs (request routing, Solr queries, and speeds)**:
  ```bash
  tail -f /var/log/stremio-easynews.log
  ```

* **Inspect error output logs (failures or panic reports)**:
  ```bash
  tail -f /var/log/stremio-easynews.err
  ```

