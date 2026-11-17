import os
import sys
import time

from tencentcloud.common import credential
from tencentcloud.common.exception.tencent_cloud_sdk_exception import TencentCloudSDKException
from tencentcloud.scf.v20180416 import models, scf_client


def configure(function_name):
    client = scf_client.ScfClient(
        credential.Credential(
            os.environ['TENCENT_SECRET_ID'],
            os.environ['TENCENT_SECRET_KEY'],
            os.environ.get('TENCENT_TOKEN'),
        ),
        os.environ['SCF_REGION'],
    )
    request = models.GetFunctionRequest()
    request.FunctionName = function_name
    request.Namespace = 'default'

    def wait_active(require_fixed=False):
        for attempt in range(60):
            function = client.GetFunction(request)
            if function.Status in ('Failed', 'CreateFailed', 'UpdateFailed'):
                raise RuntimeError('SCF function configuration failed')
            fixed = function.IntranetConfig and function.IntranetConfig.IpFixed == 'ENABLE'
            if function.Status == 'Active' and (not require_fixed or fixed):
                return function
            time.sleep(3)
        raise RuntimeError('SCF function configuration timed out')

    function = wait_active()
    if not function.IntranetConfig or function.IntranetConfig.IpFixed != 'ENABLE':
        update = models.UpdateFunctionConfigurationRequest()
        update.FunctionName = function_name
        update.Namespace = 'default'
        update.IntranetConfig = models.IntranetConfigIn()
        update.IntranetConfig.IpFixed = 'ENABLE'
        client.UpdateFunctionConfiguration(update)
        wait_active(require_fixed=True)
    print(f'Fixed intranet egress IP enabled for {function_name}', flush=True)


if __name__ == '__main__':
    try:
        if len(sys.argv) != 2:
            raise RuntimeError('Usage: scf-network.py FUNCTION_NAME')
        configure(sys.argv[1])
    except TencentCloudSDKException as error:
        print('SCF network configuration failed:', error.code, file=sys.stderr)
        sys.exit(1)
    except (KeyError, RuntimeError) as error:
        print('SCF network configuration failed:', error, file=sys.stderr)
        sys.exit(1)
