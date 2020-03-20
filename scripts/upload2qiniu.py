# coding: utf-8

import argparse
import glob
import re
import logging
import os

import qiniu.config
from qiniu import Auth, etag, put_file
from qiniu import Zone, set_default
from retry import retry

class Qiniu:
    def __init__(self, access_key, secret_key, bucket_name: str):
        self._access_key = access_key
        self._secret_key = secret_key
        self._auth = Auth(access_key, secret_key)
        self._bucket = bucket_name

    @retry(tries=5, delay=0.5, jitter=0.1, logger=logging)
    def upload_file(self, key, localfile):
        token = self._auth.upload_token(self._bucket, key)
        ret, info = put_file(token, key, localfile)
        if ret: # ret possibly is None
            assert ret['key'] == key
            assert ret['hash'] == etag(localfile)
        return info


# Get key from https://portal.qiniu.com/user/key
access_key = 'caBese-UQYXwQeigGqtdgwybP2Qh2AlDPdcEd42C' # here only-for-test
secret_key = '........'

zone = Zone(up_host='https://up.qiniup.com',
            up_host_backup='https://upload.qiniup.com',
            io_host='http://iovip.qbox.me',
            scheme='https')
set_default(default_zone=zone)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("-A",
                        '--access-key',
                        help='access key, env-var QINIU_ACCESS_KEY',
                        default=os.environ.get("QINIU_ACCESS_KEY", access_key))
    parser.add_argument("-S",
                        "--secret-key",
                        help='secret key, env-var QINIU_SECRET_KEY',
                        default=os.environ.get("QINIU_SECRET_KEY", secret_key))
    parser.add_argument("-B",
                        "--bucket",
                        help='bucket name',
                        default=os.environ.get("QINIU_BUCKET", "atxupload"))
    args = parser.parse_args()

    assert args.access_key
    assert args.secret_key
    assert args.bucket

    qn = Qiniu(args.access_key, args.secret_key, args.bucket)
    checksum_path = glob.glob("*/atx-agent_*_checksums.txt")[0]
    distdir = os.path.dirname(checksum_path)

    version = re.search(r"([\d.]+)", checksum_path).group(0)
    print(checksum_path, version)
    key_prefix = "openatx/atx-agent/releases/download/" + version + "/"
    qn.upload_file(key_prefix + os.path.basename(checksum_path), checksum_path)
    with open(checksum_path, 'r', encoding='utf-8') as f:
        for line in f:
            _, filename = line.split()
            key = key_prefix + filename
            print("-", filename, "->", key)
            qn.upload_file(key, os.path.join(distdir, filename))


if __name__ == "__main__":
    main()
