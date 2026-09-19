# topowright

`topowright` は、YAMLで定義したネットワークトポロジーから、ホストごとの
iptables `INPUT` ルールを生成するツールです。

サービスが待ち受けるポートと、サービス間またはネットワークからの接続関係を
`topology.yml` に記述すると、許可ルールと最後のDROPルールを標準出力へ出力します。
生成したコマンドを自動的に適用することはありません。

## 必要な環境

- Go 1.26.5以降

## 実行方法

リポジトリのルートに `topology.yml` を配置して実行します。

```sh
go run .
```

結果をファイルに保存する場合は、標準出力をリダイレクトします。

```sh
go run . > iptables-rules.sh
```

テストは次のコマンドで実行できます。

```sh
go test ./...
```

## 設定

`topology.yml` は `networks`、`services`、`serviceGroups` の3つのセクションで
構成されます。

### networks

サービスへの接続を許可する送信元ネットワークまたはホストを定義します。

```yaml
networks:
  bastion:
    ip: 192.168.0.1
    connects:
      - serviceName: icmp
        portName: icmp
      - serviceName: ssh
        portName: ssh
```

- `ip`: 送信元のIPアドレスまたはCIDR
- `connects[].serviceName`: 接続先サービス名
- `connects[].portName`: 接続先サービスの待ち受け名

上の例では、`192.168.0.1` からICMPとSSHへの接続を許可します。

### services

待ち受けと、別サービスへの接続関係を定義します。

```yaml
services:
  api:
    listens:
      - name: http
        port: 80
      - name: dns
        type: udp
        port: 53
    connects:
      - serviceName: db
        portName: mysql

  db:
    listens:
      - name: mysql
        port: 3306
```

`listens` の項目は次の意味を持ちます。

| 項目 | 必須 | 説明 |
| --- | --- | --- |
| `name` | はい | 接続定義から参照する待ち受け名 |
| `type` | いいえ | `tcp`、`udp`、または `icmp`。省略時は `tcp` |
| `port` | TCP/UDPのみ | 宛先ポート番号。ICMPでは指定不要 |

`connects` は、そのサービスを持つ全ホストを送信元として扱います。上の例では、
`api` サービスを持つホストから、`db` サービスの `mysql` 待ち受けへの通信が
許可されます。

ICMPはポートを持たないため、次のように定義します。

```yaml
services:
  icmp:
    listens:
      - name: icmp
        type: icmp
```

### serviceGroups

ホストと、そのホスト上で提供するサービスをグループとして定義します。

```yaml
serviceGroups:
  api:
    hosts:
      api1.example.com:
        ip: 10.0.0.1
      api2.example.com:
        ip: 10.0.0.2
    services:
      - icmp
      - ssh
      - api
```

各ホストには、`services` に列挙したサービス向けのルールが出力されます。

## 生成例

TCPではプロトコルと宛先ポートを指定します。

```sh
iptables -A INPUT -s 192.168.0.1 -p tcp --dport 22 -j ACCEPT -m comment --comment "from bastion to ssh"
```

ICMPでは宛先ポートを付けず、プロトコルだけを指定します。

```sh
iptables -A INPUT -s 192.168.0.1 -p icmp -j ACCEPT -m comment --comment "from bastion to icmp"
```

各ホストの許可ルールの末尾には、次のDROPルールが出力されます。

```sh
iptables -A INPUT -j DROP
```

## 自己IPの除外

サービス間接続によって送信元IPが出力対象ホスト自身のIPと一致する場合、その
ルールは出力されません。例えば `10.0.0.1` 向けのルールセットには、送信元が
`10.0.0.1` の許可ルールを追加しません。

この判定はIP文字列の完全一致で行います。CIDRに対象ホストのIPが含まれるかどうかは
判定しません。

## 注意事項

- `topology.yml` は実行時のカレントディレクトリから読み込みます。
- 対応する通信タイプはTCP、UDP、ICMPです。それ以外を指定するとエラーになります。
- `connects[].portName` は、接続先サービスの `listens[].name` と一致させてください。
- 出力されるコマンドの適用前に、意図したルールになっていることを確認してください。
