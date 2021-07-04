# Idempotency Key
IK=$(openssl rand -hex 32)
DATA='{"title": "title-1", "artist": "artist-1", "year": 2019}'
echo $DATA
IK2=$(openssl rand -hex 32)
DATA2='{"title": "title-2", "artist": "artist-2", "year": 2021}'
# Sending consecutive requests.
curl -i -H "Idempotency-Key:$IK" -X POST -d "$DATA" localhost:8080/songs/create &
curl -i -H "Idempotency-Key:$IK2" -X POST -d "$DATA2" localhost:8080/songs/create &
curl -i -H "Idempotency-Key:$IK" -X POST -d "$DATA" localhost:8080/songs/create &
curl -i -H "Idempotency-Key:$IK2" -X POST -d "$DATA2" localhost:8080/songs/create &
curl -i -H "Idempotency-Key:$IK" -X POST -d "$DATA" localhost:8080/songs/create
curl -i -H "Idempotency-Key:$IK2" -X POST -d "$DATA2" localhost:8080/songs/create &

# Wait for create song done for better output.
sleep 1
echo "Get songs"
curl -i localhost:8080/songs/list
