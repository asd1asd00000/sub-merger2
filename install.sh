#!/bin/bash
# Sub-Merger Interactive Auto-Installer (Pro Hardcoded Edition)
# -------------------------------------------------------------
# Anti-Sanction & Stability Configuration
# -------------------------------------------------------------
export GOTOOLCHAIN=local
export GOPROXY=https://goproxy.io,direct
export GOSUMDB=off

# Color variables for terminal
C_DEF='\033[0m'
C_GREEN='\033[1;32m'
C_CYAN='\033[1;36m'
C_YELLOW='\033[1;33m'
C_RED='\033[1;31m'
C_BOX='\033[38;5;63m'

echo -e "${C_CYAN}=================================================${C_DEF}"
echo -e " 🚀 ${C_GREEN}Sub-Merger Installer & Manager${C_DEF} 🚀 "
echo -e "${C_CYAN}=================================================${C_DEF}"

echo -e "What would you like to do?"
echo -e " ${C_GREEN}1)${C_DEF} Install or Update Sub-Merger Panel"
echo -e " ${C_RED}2)${C_DEF} Completely Uninstall & Clean Server"
echo -e "${C_CYAN}-------------------------------------------------${C_DEF}"

read -p "Select an option [1-2]: " MENU_OPTION

# =============================================================
# UNINSTALLATION BLOCK
# =============================================================
if [ "$MENU_OPTION" == "2" ]; then
    echo -e "\n🗑️ ${C_YELLOW}Starting complete uninstallation...${C_DEF}"
    echo -e "1️⃣ ${C_CYAN}Stopping and removing Systemd service...${C_DEF}"
    systemctl stop sub-merger.service > /dev/null 2>&1
    systemctl disable sub-merger.service > /dev/null 2>&1
    rm -f /etc/systemd/system/sub-merger.service
    systemctl daemon-reload

    echo -e "2️⃣ ${C_CYAN}Cleaning Nginx configs and revoking SSL...${C_DEF}"
    if [ -f /etc/nginx/sites-available/sub-merger ]; then
        DOMAIN_TO_REMOVE=$(grep server_name /etc/nginx/sites-available/sub-merger | awk '{print $2}' | tr -d ';')
        if [ ! -z "$DOMAIN_TO_REMOVE" ]; then
            certbot delete --cert-name $DOMAIN_TO_REMOVE --non-interactive > /dev/null 2>&1
        fi
        rm -f /etc/nginx/sites-available/sub-merger
        rm -f /etc/nginx/sites-enabled/sub-merger
        systemctl restart nginx
    fi

    echo -e "3️⃣ ${C_CYAN}Removing binary application files...${C_DEF}"
    rm -f /usr/local/bin/sub-merger-app

    echo -e "4️⃣ ${C_CYAN}Deleting database and configuration folder...${C_DEF}"
    rm -rf /etc/merge_subs/

    echo -e "5️⃣ ${C_CYAN}Freeing up ports in firewall (UFW)...${C_DEF}"
    ufw delete allow 5000/tcp > /dev/null 2>&1

    echo -e "\n✅ ${C_GREEN}Sub-Merger has been completely wiped from your server!${C_DEF}"
    echo -e "💡 ${C_YELLOW}You can now run the script again for a fresh installation without conflicts.${C_DEF}\n"
    exit 0
fi

# =============================================================
# INSTALLATION BLOCK
# =============================================================
if [ "$MENU_OPTION" != "1" ]; then
    echo -e "❌ ${C_RED}Invalid option selected. Exiting.${C_DEF}"
    exit 1
fi

echo -e "\n⚙️ ${C_GREEN}Starting Installation...${C_DEF}"

# ====================== پیش‌نیاز git ======================
echo -e "🔄 ${C_CYAN}Updating package list and installing git...${C_DEF}"
apt-get update -qq
apt-get install -y git

# 1. Get user input with default values
read -p "👤 Enter Admin Username [admin]: " ADMIN_USER
ADMIN_USER=${ADMIN_USER:-admin}

DEFAULT_PASS=$(tr -dc A-Za-z0-9 </dev/urandom | head -c 12)
read -p "🔑 Enter Admin Password [$DEFAULT_PASS]: " ADMIN_PASS
ADMIN_PASS=${ADMIN_PASS:-$DEFAULT_PASS}

echo "-------------------------------------------------"
echo "⚠️ Note: If you want to install SSL, your subdomain MUST be pointed to this server's IP."
read -p "🌐 Enter Subdomain (e.g., sub.domain.com) [Leave blank for IP only]: " DOMAIN
echo "================================================="

# 2. Install dependencies
echo -e "📥 ${C_CYAN}Installing required packages...${C_DEF}"
apt-get update && apt-get install -y zip git wget curl jq nginx certbot python3-certbot-nginx

# 3. Install Go (Hardcoded to 1.22)
export PATH=$PATH:/snap/bin:/usr/local/go/bin
if ! command -v go &> /dev/null; then
    echo -e "📥 ${C_CYAN}Installing Go 1.22...${C_DEF}"
    snap install go --channel=1.22/stable --classic || apt-get install -y golang-go
    
    export PATH=$PATH:/snap/bin:/usr/local/go/bin
    echo 'export PATH=$PATH:/snap/bin:/usr/local/go/bin' >> ~/.bashrc
    echo 'export GOTOOLCHAIN=local' >> ~/.bashrc
    echo 'export GOPROXY=https://goproxy.io,direct' >> ~/.bashrc
    echo 'export GOSUMDB=off' >> ~/.bashrc
fi

# 4. Create settings directory and json
echo -e "⚙️ ${C_CYAN}Configuring database and credentials...${C_DEF}"
mkdir -p /etc/merge_subs
cat <<EOF > /etc/merge_subs/settings.json
{
  "admin_username": "$ADMIN_USER",
  "admin_password": "$ADMIN_PASS",
  "token": "",
  "chat_id": "",
  "password": "",
  "tutorials_url": "",
  "announcements_url": "",
  "smtp_email": "",
  "smtp_password": "",
  "smtp_receiver": ""
}
EOF

# 5. Build the Go project
echo -e "⚙️ ${C_CYAN}Building the core application...${C_DEF}"
go clean -modcache
go mod edit -go=1.22.0
go mod edit -toolchain=none
go mod tidy
go build -o /usr/local/bin/sub-merger-app cmd/server/main.go
chmod +x /usr/local/bin/sub-merger-app

# 6. Create and start Systemd service
echo -e "🛠️ ${C_CYAN}Setting up Systemd service...${C_DEF}"
cat <<EOF > /etc/systemd/system/sub-merger.service
[Unit]
Description=Sub-Merger Panel Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/etc/merge_subs
ExecStart=/usr/local/bin/sub-merger-app
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable sub-merger.service
systemctl restart sub-merger.service

# 7. Configure Nginx and SSL
FINAL_URL="http://$(curl -s ifconfig.me):5000/admin"
if [ ! -z "$DOMAIN" ]; then
    echo -e "🌍 ${C_CYAN}Configuring Nginx and fetching SSL for $DOMAIN...${C_DEF}"
   
    rm -f /etc/nginx/sites-enabled/default
    cat <<EOF > /etc/nginx/sites-available/sub-merger
server {
    listen 80;
    server_name $DOMAIN;
    location / {
        proxy_pass http://127.0.0.1:5000;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF
    ln -s /etc/nginx/sites-available/sub-merger /etc/nginx/sites-enabled/
    systemctl restart nginx
    certbot --nginx -d $DOMAIN --non-interactive --agree-tos -m admin@$DOMAIN --redirect
    FINAL_URL="https://$DOMAIN/admin"
else
    ufw allow 5000/tcp > /dev/null 2>&1
fi

# 8. Final Message
echo -e ""
echo -e "${C_BOX}╭──────────────────────────────────────────────────────────────────────╮${C_DEF}"
echo -e "${C_BOX}│${C_DEF} ${C_GREEN}✅ Sub-Merger Panel Installed Successfully!${C_DEF} ${C_BOX}│${C_DEF}"
echo -e "${C_BOX}├──────────────────────────────────────────────────────────────────────┤${C_DEF}"
printf "${C_BOX}│${C_DEF} %b %-55s ${C_BOX}│\n${C_DEF}" "🌐 ${C_CYAN}URL: ${C_DEF}" "$FINAL_URL"
printf "${C_BOX}│${C_DEF} %b %-55s ${C_BOX}│\n${C_DEF}" "👤 ${C_YELLOW}Username: ${C_DEF}" "$ADMIN_USER"
printf "${C_BOX}│${C_DEF} %b %-55s ${C_BOX}│\n${C_DEF}" "🔑 ${C_YELLOW}Password: ${C_DEF}" "$ADMIN_PASS"
echo -e "${C_BOX}╰──────────────────────────────────────────────────────────────────────╯${C_DEF}"
echo -e "💡 ${C_GREEN}Note:${C_DEF} Please save these credentials in a safe place."
echo -e ""
